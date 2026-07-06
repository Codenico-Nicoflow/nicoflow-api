package optional

import (
	"encoding/json"
	"testing"
)

type payload struct {
	Name Field[string] `json:"name"`
}

func TestField_Absent(t *testing.T) {
	var p payload
	if err := json.Unmarshal([]byte(`{}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Name.Set {
		t.Error("absent key: Set = true, want false")
	}
	if _, ok := p.Name.Get(); ok {
		t.Error("absent key: Get ok = true, want false")
	}
}

func TestField_ExplicitNull(t *testing.T) {
	var p payload
	if err := json.Unmarshal([]byte(`{"name":null}`), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Name.Set {
		t.Error("null: Set = false, want true (present but null)")
	}
	if p.Name.Value != nil {
		t.Error("null: Value != nil, want nil")
	}
	if _, ok := p.Name.Get(); ok {
		t.Error("null: Get ok = true, want false")
	}
}

func TestField_Value(t *testing.T) {
	var p payload
	if err := json.Unmarshal([]byte(`{"name":"hi"}`), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Name.Set {
		t.Error("value: Set = false, want true")
	}
	got, ok := p.Name.Get()
	if !ok || got != "hi" {
		t.Errorf("value: Get = %q,%v, want \"hi\",true", got, ok)
	}
}

func TestField_InvalidJSON(t *testing.T) {
	var p payload
	if err := json.Unmarshal([]byte(`{"name":123}`), &p); err == nil {
		t.Error("string field with number: want unmarshal error, got nil")
	}
}
