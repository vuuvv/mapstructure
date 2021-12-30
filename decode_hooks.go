package mapstructure

import (
	"encoding"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// typedDecodeHook takes a raw DecodeHookFunc (an interface{}) and turns
// it into the proper DecodeHookFunc type, such as DecodeHookFuncType.
func typedDecodeHook(h DecodeHookFunc) DecodeHookFunc {
	// Create variables here so we can reference them with the reflect pkg
	var f1 DecodeHookFuncType
	var f2 DecodeHookFuncKind
	var f3 DecodeHookFuncValue
	var f4 DecodeHookFuncValueWithName

	// Fill in the variables into this interface and the rest is done
	// automatically using the reflect package.
	potential := []interface{}{f1, f2, f3, f4}

	v := reflect.ValueOf(h)
	vt := v.Type()
	for _, raw := range potential {
		pt := reflect.ValueOf(raw).Type()
		if vt.ConvertibleTo(pt) {
			return v.Convert(pt).Interface()
		}
	}

	return nil
}

// DecodeHookExec executes the given decode hook. This should be used
// since it'll naturally degrade to the older backwards compatible DecodeHookFunc
// that took reflect.Kind instead of reflect.Type.
func DecodeHookExec(
	raw DecodeHookFunc,
	name string,
	from reflect.Value, to reflect.Value) (interface{}, error) {

	switch f := typedDecodeHook(raw).(type) {
	case DecodeHookFuncType:
		return f(from.Type(), to.Type(), from.Interface())
	case DecodeHookFuncKind:
		return f(from.Kind(), to.Kind(), from.Interface())
	case DecodeHookFuncValue:
		return f(from, to)
	case DecodeHookFuncValueWithName:
		return f(name, from, to)
	default:
		return nil, errors.New("invalid decode hook signature")
	}
}

// ComposeDecodeHookFunc creates a single DecodeHookFunc that
// automatically composes multiple DecodeHookFuncs.
//
// The composed funcs are called in order, with the result of the
// previous transformation.
func ComposeDecodeHookFunc(fs ...DecodeHookFunc) DecodeHookFunc {
	return func(name string, f reflect.Value, t reflect.Value) (interface{}, error) {
		var err error
		data := f.Interface()

		newFrom := f
		for _, f1 := range fs {
			data, err = DecodeHookExec(f1, name, newFrom, t)
			if err != nil {
				return nil, err
			}
			newFrom = reflect.ValueOf(data)
		}

		return data, nil
	}
}

// StringToSliceHookFunc returns a DecodeHookFunc that converts
// string to []string by splitting on the given sep.
func StringToSliceHookFunc(sep string) DecodeHookFunc {
	return func(
		f reflect.Kind,
		t reflect.Kind,
		data interface{}) (interface{}, error) {
		if f != reflect.String || t != reflect.Slice {
			return data, nil
		}

		raw := data.(string)
		if raw == "" {
			return []string{}, nil
		}

		return strings.Split(raw, sep), nil
	}
}

// StringToTimeDurationHookFunc returns a DecodeHookFunc that converts
// strings to time.Duration.
func StringToTimeDurationHookFunc() DecodeHookFunc {
	return func(
		f reflect.Type,
		t reflect.Type,
		data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}
		if t != reflect.TypeOf(time.Duration(5)) {
			return data, nil
		}

		// Convert it by parsing
		return time.ParseDuration(data.(string))
	}
}

// StringToIPHookFunc returns a DecodeHookFunc that converts
// strings to net.IP
func StringToIPHookFunc() DecodeHookFunc {
	return func(
		f reflect.Type,
		t reflect.Type,
		data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}
		if t != reflect.TypeOf(net.IP{}) {
			return data, nil
		}

		// Convert it by parsing
		ip := net.ParseIP(data.(string))
		if ip == nil {
			return net.IP{}, fmt.Errorf("failed parsing ip %v", data)
		}

		return ip, nil
	}
}

// StringToIPNetHookFunc returns a DecodeHookFunc that converts
// strings to net.IPNet
func StringToIPNetHookFunc() DecodeHookFunc {
	return func(
		f reflect.Type,
		t reflect.Type,
		data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}
		if t != reflect.TypeOf(net.IPNet{}) {
			return data, nil
		}

		// Convert it by parsing
		_, net, err := net.ParseCIDR(data.(string))
		return net, err
	}
}

// StringToTimeHookFunc returns a DecodeHookFunc that converts
// strings to time.Time.
func StringToTimeHookFunc(layout string) DecodeHookFunc {
	return func(
		f reflect.Type,
		t reflect.Type,
		data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}
		if t != reflect.TypeOf(time.Time{}) {
			return data, nil
		}

		// Convert it by parsing
		return time.Parse(layout, data.(string))
	}
}

// WeaklyTypedHook is a DecodeHookFunc which adds support for weak typing to
// the decoder.
//
// Note that this is significantly different from the WeaklyTypedInput option
// of the DecoderConfig.
func WeaklyTypedHook(
	f reflect.Kind,
	t reflect.Kind,
	data interface{}) (interface{}, error) {
	dataVal := reflect.ValueOf(data)
	switch t {
	case reflect.String:
		switch f {
		case reflect.Bool:
			if dataVal.Bool() {
				return "1", nil
			}
			return "0", nil
		case reflect.Float32:
			return strconv.FormatFloat(dataVal.Float(), 'f', -1, 64), nil
		case reflect.Int:
			return strconv.FormatInt(dataVal.Int(), 10), nil
		case reflect.Slice:
			dataType := dataVal.Type()
			elemKind := dataType.Elem().Kind()
			if elemKind == reflect.Uint8 {
				return string(dataVal.Interface().([]uint8)), nil
			}
		case reflect.Uint:
			return strconv.FormatUint(dataVal.Uint(), 10), nil
		}
	}

	return data, nil
}

func RecursiveStructToMapHookFunc() DecodeHookFunc {
	return func(f reflect.Value, t reflect.Value) (interface{}, error) {
		if f.Kind() != reflect.Struct {
			return f.Interface(), nil
		}

		var i interface{} = struct{}{}
		if t.Type() != reflect.TypeOf(&i).Elem() {
			return f.Interface(), nil
		}

		m := make(map[string]interface{})
		t.Set(reflect.ValueOf(m))

		return f.Interface(), nil
	}
}

func SystemEnvironmentHookFunc(prefix ...string) DecodeHookFunc {
	p := ""
	if len(prefix) != 0 {
		p = strings.Join(prefix, "")
	}
	return func(name string, f reflect.Value, t reflect.Value) (interface{}, error) {
		// 环境变量不能设置结构体、map、slice、array
		if t.Kind() == reflect.Struct ||
			t.Kind() == reflect.Map ||
			t.Kind() == reflect.Slice ||
			t.Kind() == reflect.Array {
			return f.Interface(), nil
		}

		envName := name
		if len(name) != 0 {
			envName = p + "." + name
		}
		env, ok := getEnv(envName)
		if ok {
			return env, nil
		}
		return f.Interface(), nil
	}
}

func getEnv(name string) (val string, ok bool) {
	val, ok = os.LookupEnv(name)
	if ok {
		return
	}

	upperName := strings.ToUpper(name)
	val, ok = os.LookupEnv(name)
	if ok {
		return
	}

	val, ok = getEnvRelax(upperName)
	if ok {
		return
	}

	val, ok = getEnvRelax(name)
	if ok {
		return
	}
	return "", false
}

func getEnvRelax(name string) (val string, ok bool) {
	noDotName := strings.ReplaceAll(name, ".", "_")
	if name != noDotName {
		if val, ok = os.LookupEnv(noDotName); ok {
			return
		}
	}

	noHyphenName := strings.ReplaceAll(name, "-", "_")
	if name != noHyphenName {
		if val, ok = os.LookupEnv(noHyphenName); ok {
			return
		}
	}

	noDotNoHyphenName := strings.ReplaceAll(noHyphenName, ".", "_")
	if name != noHyphenName {
		if val, ok = os.LookupEnv(noDotNoHyphenName); ok {
			return
		}
	}
	return "", false
}

// TextUnmarshallerHookFunc returns a DecodeHookFunc that applies
// strings to the UnmarshalText function, when the target type
// implements the encoding.TextUnmarshaler interface
func TextUnmarshallerHookFunc() DecodeHookFuncType {
	return func(
		f reflect.Type,
		t reflect.Type,
		data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}
		result := reflect.New(t).Interface()
		unmarshaller, ok := result.(encoding.TextUnmarshaler)
		if !ok {
			return data, nil
		}
		if err := unmarshaller.UnmarshalText([]byte(data.(string))); err != nil {
			return nil, err
		}
		return result, nil
	}
}
