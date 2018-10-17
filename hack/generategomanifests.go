package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"

	v1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
)

type TypeSig struct {
	str   string
	empty bool
}

func translateRecursive(original reflect.Value, indent int) *TypeSig {
	switch original.Kind() {
	// The first cases handle nested structures and translate them recursively

	// If it is a pointer we need to unwrap and call once again
	case reflect.Ptr:
		// To get the actual value of the original we have to call Elem()
		// At the same time this unwraps the pointer so we don't end up in
		// an infinite recursion
		originalValue := original.Elem()
		// Check if the pointer is nil
		if !originalValue.IsValid() {
			return &TypeSig{str: fmt.Sprintf("%v(nil)", original.Type().String()), empty: true}
		}
		// Allocate a new object and set the pointer to it
		//copy.Set(reflect.New(originalValue.Type()))
		// Unwrap the newly created pointer
		typeSig := translateRecursive(originalValue, indent+1)
		// TODO(jchaloup): Wrap (*type)DATA
		typeSig.str = "&" + typeSig.str
		return typeSig

	// If it is an interface (which is very similar to a pointer), do basically the
	// same as for the pointer. Though a pointer is not the same as an interface so
	// note that we have to call Elem() after creating a new object because otherwise
	// we would end up with an actual pointer
	case reflect.Interface:
		// Get rid of the wrapping interface
		originalValue := original.Elem()
		// Create a new object. Now new gives us a pointer, but we want the value it
		// points to, so we have to call Elem() to unwrap it
		// copyValue := reflect.New(originalValue.Type()).Elem()
		fmt.Printf("reflect.Interface\n")
		return translateRecursive(originalValue, indent+1)
		// copy.Set(copyValue)

	// If it is a struct we translate each field
	case reflect.Struct:
		if original.NumField() == 0 {
			return &TypeSig{str: fmt.Sprintf("%v{nil}", original.Type().String()), empty: true}
		}
		var items []string
		empty := true
		for i := 0; i < original.NumField(); i += 1 {
			item := translateRecursive(original.Field(i), indent+1)
			if item.empty {
				continue
			}
			empty = false
			items = append(items, fmt.Sprintf("%v%v: %v,", strings.Repeat("\t", indent+1), original.Type().Field(i).Name, item.str))
		}
		if empty {
			return &TypeSig{str: fmt.Sprintf("%v{nil}", original.Type().String()), empty: true}
		}
		return &TypeSig{str: fmt.Sprintf("%v{\n%v\n%v}", original.Type().String(), strings.Join(items, "\n"), strings.Repeat("\t", indent))}

	// If it is a slice we create a new slice and translate each element
	case reflect.Slice:
		if original.Len() == 0 {
			return &TypeSig{str: fmt.Sprintf("%v{nil}", original.Type().String()), empty: true}
		}

		var items []string
		empty := true
		for i := 0; i < original.Len(); i += 1 {
			item := translateRecursive(original.Index(i), indent+1)
			if item.empty {
				continue
			}
			empty = false
			items = append(items, fmt.Sprintf("%v%v,", strings.Repeat("\t", indent+1), item.str))
		}

		if empty {
			return &TypeSig{str: fmt.Sprintf("%v{nil}", original.Type().String()), empty: true}
		}

		return &TypeSig{str: fmt.Sprintf("%v{\n%v\n%v}", original.Type().String(), strings.Join(items, "\n"), strings.Repeat("\t", indent))}

	// If it is a map we create a new map and translate each value
	case reflect.Map:
		if len(original.MapKeys()) == 0 {
			return &TypeSig{str: fmt.Sprintf("%v{nil}", original.Type().String()), empty: true}
		}
		var items []string
		for _, key := range original.MapKeys() {
			originalValue := original.MapIndex(key)
			// New gives us a pointer, but again we want the value
			item := translateRecursive(originalValue, indent)
			items = append(items, fmt.Sprintf("%v\"%v\": %v,", strings.Repeat("\t", indent), key, item.str))
		}
		return &TypeSig{str: fmt.Sprintf("%v{\n%v\n%v}", original.Type().String(), strings.Join(items, "\n"), strings.Repeat("\t", indent))}

	// Otherwise we cannot traverse anywhere so this finishes the the recursion

	// If it is a string translate it (yay finally we're doing what we came for)
	case reflect.String:
		empty := false
		switch original.Interface().(type) {
		case string:
			if original.Interface().(string) == "" {
				empty = true
			}
			return &TypeSig{str: fmt.Sprintf("\"%v\"", original.Interface().(string)), empty: empty}
		case v1beta1.JSONSchemaURL:
			if original.Interface().(v1beta1.JSONSchemaURL) == "" {
				empty = true
			}
			return &TypeSig{str: fmt.Sprintf("%v%v(\"%v\")", strings.Repeat("\t", indent), original.Type().String(), original.Interface().(v1beta1.JSONSchemaURL)), empty: empty}
		case types.UID:
			if original.Interface().(types.UID) == "" {
				empty = true
			}
			return &TypeSig{str: fmt.Sprintf("%v%v(\"%v\")", strings.Repeat("\t", indent), original.Type().String(), original.Interface().(types.UID)), empty: empty}
		case v1beta1.ResourceScope:
			if original.Interface().(v1beta1.ResourceScope) == "" {
				empty = true
			}
			return &TypeSig{str: fmt.Sprintf("%v%v(\"%v\")", strings.Repeat("\t", indent), original.Type().String(), original.Interface().(v1beta1.ResourceScope)), empty: empty}
		default:
			fmt.Printf("%#v\n", original.Type().String())
			panic("Just fail!!!")
		}

	// And everything else will simply be taken from the original
	default:
		switch original.Kind().String() {
		case "bool":
			return &TypeSig{str: "false", empty: true}
		case "int64", "uint64":
			return &TypeSig{str: "0", empty: true}
		}
		panic(fmt.Errorf("default: %v\n", original.Kind().String()))
	}
	panic(fmt.Errorf("Ha: %v\n", original.Kind()))
}

func main() {

	manifest, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		panic(err)
	}

	scheme := runtime.NewScheme()
	v1beta1.AddToScheme(scheme)
	codecFactory := serializer.NewCodecFactory(scheme)
	decoder := codecFactory.UniversalDecoder(v1beta1.SchemeGroupVersion)

	crd := &v1beta1.CustomResourceDefinition{}

	decoder.Decode(manifest, nil, crd)
	fmt.Printf("%v\n", translateRecursive(reflect.ValueOf(crd), 0).str)
}
