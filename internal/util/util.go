package util

import (
	"strconv"
	"strings"
)

// Collapse recursively traverses a map and tries to collapse it to a flat
// slice of `key.key.key=value` pairs
func Collapse(x interface{}, path []string, acc []string) []string {
	if path == nil {
		path = []string{}
	}
	if acc == nil {
		acc = []string{}
	}

	switch x := x.(type) {
	case map[interface{}]interface{}:
		var arr []string
		for key, value := range x {
			newPath := append(path, key.(string))
			arr = append(arr, Collapse(value, newPath, acc)...)
		}
		return arr
	case bool:
		return append(acc, strings.Join(path, ".")+"="+strconv.FormatBool(x))
	case float64:
		return append(acc, strings.Join(path, ".")+"="+strconv.FormatFloat(x, 'f', -1, 64))
	case string:
		return append(acc, strings.Join(path, ".")+"="+string(x))
	case int:
		return append(acc, strings.Join(path, ".")+"="+string(x))
	default:
		// Just exclude datatypes we don't know about. It's possible this isn't
		// handling all the cases that yaml parsing can provide
		return acc
	}
}
