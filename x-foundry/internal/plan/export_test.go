package plan_test

import (
	"reflect"
	"strings"
)

// jsonKeys returns the JSON keys declared by a struct (following embedded structs).
func jsonKeys(v any) map[string]bool {
	out := map[string]bool{}
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	for i := 0; i < t.NumField(); i++ {
		if key := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]; key != "" && key != "-" {
			out[key] = true
		}
	}
	return out
}
