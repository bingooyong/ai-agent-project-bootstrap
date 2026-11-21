package strictio

import "testing"

func TestDecodeRejectsUnsafeDocuments(t *testing.T) {
	type value struct {
		Name string `yaml:"name"`
	}
	for _, input := range []string{"name: a\nname: b", "unknown: a", "name: &x a", "name: a\n---\nname: b", "name: null", "name: !!binary a", "{\"name\":\"a\",\"name\":\"b\"}"} {
		t.Run(input, func(t *testing.T) {
			var v value
			if Decode([]byte(input), &v) == nil {
				t.Fatal("accepted unsafe document")
			}
		})
	}
	for _, input := range []string{"name: 1", "name: true", "name: []", "name: {}"} {
		var v value
		if Decode([]byte(input), &v) == nil {
			t.Errorf("scalar coercion accepted %s", input)
		}
	}
	for _, input := range []string{"name: ok", `{"name":"ok"}`} {
		var v value
		if err := Decode([]byte(input), &v); err != nil || v.Name != "ok" {
			t.Fatalf("%s: %v %#v", input, err, v)
		}
	}
}
