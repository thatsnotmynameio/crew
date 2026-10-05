package fakegithub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/itchyny/gojq"
)

// object is a JSON object whose keys keep the order they were set in, as
// GitHub writes them.
type object []member

// member is one key of an object and its value.
type member struct {
	key   string
	value any
}

// MarshalJSON implements json.Marshaler: the members in order, without
// HTML escaping.
func (o object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := encode(m.key)
		if err != nil {
			return nil, err
		}
		v, err := encode(m.value)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// set returns o with key holding value, in its place when o already has
// key, and last otherwise.
func (o object) set(key string, value any) object {
	for i := range o {
		if o[i].key == key {
			o[i].value = value
			return o
		}
	}
	return append(o, member{key, value})
}

// encode returns v as one line of compact JSON without HTML escaping, as
// GitHub writes it.
func encode(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// output returns the reply of a gh call that printed body, JSON, as is, or
// through the jq program when it is not "", as gh's --jq does. A trailing
// newline ends the JSON when newline is set, as gh's --json output has.
func output(body any, program string, env map[string]string, newline bool) Reply {
	data, err := encode(body)
	if err != nil {
		return failed(err.Error())
	}
	if program == "" {
		if newline {
			data = append(data, '\n')
		}
		return Reply{Stdout: data}
	}
	out, err := jq(data, program, env)
	if err != nil {
		return failed(err.Error())
	}
	return printed(out)
}

// jq runs program on input, JSON, with gojq as gh does, and returns what gh
// prints: each result on its own line, strings unquoted.
func jq(input []byte, program string, env map[string]string) (string, error) {
	query, err := gojq.Parse(program)
	if err != nil {
		return "", fmt.Errorf("parse jq expression: %w", err)
	}
	environ := make([]string, 0, len(env))
	for k, v := range env {
		environ = append(environ, k+"="+v)
	}
	code, err := gojq.Compile(query, gojq.WithEnvironLoader(func() []string { return environ }))
	if err != nil {
		return "", fmt.Errorf("compile jq expression: %w", err)
	}
	var data any
	if err := json.Unmarshal(input, &data); err != nil {
		return "", fmt.Errorf("read JSON: %w", err)
	}
	var out strings.Builder
	iter := code.Run(data)
	for v, ok := iter.Next(); ok; v, ok = iter.Next() {
		if err, isErr := v.(error); isErr {
			return "", fmt.Errorf("jq: %w", err)
		}
		text, err := jqText(v)
		if err != nil {
			return "", err
		}
		out.WriteString(text + "\n")
	}
	return out.String(), nil
}

// jqText returns a jq result as gh prints it: a string as it is, an
// integral number without decimals, null as nothing, and anything else as
// compact JSON.
func jqText(v any) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	if f, ok := v.(float64); ok {
		if math.Trunc(f) == f {
			return strconv.FormatFloat(f, 'f', 0, 64), nil
		}
		return strconv.FormatFloat(f, 'f', 2, 64), nil
	}
	if v == nil {
		return "", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode jq result: %w", err)
	}
	return string(b), nil
}
