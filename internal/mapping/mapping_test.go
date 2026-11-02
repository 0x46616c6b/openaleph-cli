package mapping

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const peopleCSV = `Vorname,Name,Strasse,Ort,Art,Tel
Anna,Beispiel,Hauptstr. 1,Leipzig,Int.,0341 1;0341 2
Bernd,Muster,Hauptstr. 1,Leipzig,Int.,
,,,,,
Cora,Test,,Dresden,Gast,
`

const peopleMapping = `
dataset: demo
queries:
  - csv_url: people.csv
    filters_not:
      Art: Gast
    entities:
      address:
        schema: Address
        keys: [Strasse, Ort]
        properties:
          full:
            columns: [Strasse, Ort]
            join: ", "
          city:
            column: Ort
          country:
            literal: de
      person:
        schema: Person
        keys: [Vorname, Name]
        properties:
          firstName:
            column: Vorname
            required: true
          lastName:
            column: Name
          phone:
            column: Tel
            split: ";"
          addressEntity:
            entity: address
`

func run(t *testing.T, mappingYAML, csvName, csvData string) ([]Entity, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, csvName), []byte(csvData), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Parse([]byte(mappingYAML))
	if err != nil {
		return nil, err
	}
	var out []Entity
	err = m.Run(context.Background(), dir, func(e Entity) error {
		out = append(out, e)
		return nil
	})
	return out, err
}

func TestRunMapsPeopleAndSharedAddress(t *testing.T) {
	got, err := run(t, peopleMapping, "people.csv", peopleCSV)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Entity{}
	var addresses, people int
	for _, e := range got {
		switch e.Schema {
		case "Address":
			addresses++
		case "Person":
			people++
			byName[e.Properties["lastName"][0]] = e
		}
	}
	// the blank row and the filtered "Gast" row are dropped; both people share one address
	if people != 2 || addresses != 1 {
		t.Fatalf("people=%d addresses=%d, want 2 and 1: %+v", people, addresses, got)
	}
	anna := byName["Beispiel"]
	if !reflect.DeepEqual(anna.Properties["phone"], []string{"0341 1", "0341 2"}) {
		t.Errorf("phone = %v", anna.Properties["phone"])
	}
	if anna.Properties["addressEntity"][0] != byName["Muster"].Properties["addressEntity"][0] {
		t.Error("people on the same street must reference the same address ID")
	}
	for _, e := range got {
		if e.Schema == "Address" {
			if e.ID != anna.Properties["addressEntity"][0] {
				t.Errorf("address ID %s is not the reference %s", e.ID, anna.Properties["addressEntity"][0])
			}
			if e.Properties["full"][0] != "Hauptstr. 1, Leipzig" || e.Properties["country"][0] != "de" {
				t.Errorf("address = %v", e.Properties)
			}
		}
	}
}

func TestRunIDsAreStableAndDatasetScoped(t *testing.T) {
	a, _ := run(t, peopleMapping, "people.csv", peopleCSV)
	b, _ := run(t, peopleMapping, "people.csv", peopleCSV)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("output must be deterministic")
	}
	other, _ := run(t, strings.Replace(peopleMapping, "dataset: demo", "dataset: other", 1), "people.csv", peopleCSV)
	if a[0].ID == other[0].ID {
		t.Fatal("dataset must change the entity IDs")
	}
}

func TestRunRequiredDropsEntity(t *testing.T) {
	got, err := run(t, peopleMapping, "people.csv", "Vorname,Name,Strasse,Ort,Art,Tel\n,Solo,Weg 1,Bonn,Int.,\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if e.Schema == "Person" {
			t.Fatalf("person without firstName must be dropped: %+v", e)
		}
	}
}

func TestRunReplace(t *testing.T) {
	m := `
queries:
  - csv_url: c.csv
    entities:
      p:
        schema: Person
        keys: [n]
        properties:
          name: {column: n}
          gender: {column: g, replace: {m: male, w: female}}
`
	got, err := run(t, m, "c.csv", "n,g\nA,m\nB,w\n")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Properties["gender"][0] != "male" || got[1].Properties["gender"][0] != "female" {
		t.Fatalf("got %+v", got)
	}
}

func TestRunUnknownColumn(t *testing.T) {
	m := strings.Replace(peopleMapping, "column: Vorname", "column: Vornam", 1)
	_, err := run(t, m, "people.csv", peopleCSV)
	if err == nil || !strings.Contains(err.Error(), "Vornam") || !strings.Contains(err.Error(), "header has") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseValidation(t *testing.T) {
	for name, y := range map[string]string{
		"no queries": "dataset: x\n",
		"no csv_url": "queries:\n  - entities:\n      p: {schema: Person, keys: [a]}\n",
		"no schema":  "queries:\n  - csv_url: a.csv\n    entities:\n      p: {keys: [a]}\n",
		"no keys":    "queries:\n  - csv_url: a.csv\n    entities:\n      p: {schema: Person}\n",
		"two sources": "queries:\n  - csv_url: a.csv\n    entities:\n      p:\n        schema: Person\n        keys: [a]\n" +
			"        properties:\n          name: {column: a, literal: b}\n",
		"unknown ref": "queries:\n  - csv_url: a.csv\n    entities:\n      p:\n        schema: Person\n        keys: [a]\n" +
			"        properties:\n          addressEntity: {entity: nope}\n",
	} {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
