package config

import (
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/iml885203/orbit/internal/suggest"
)

var unknownSchemaFieldPattern = regexp.MustCompile(`field ([^ \n]+) not found in type ([^ \n]+)`)

var unknownSchemaFieldPhrase = regexp.MustCompile(`field ([^ \n]+) not found in type `)

var schemaTypeNamePattern = regexp.MustCompile(`\b(?:map\[string\])?(config\.[A-Za-z0-9_]+)`)

type schemaGuidanceError struct {
	err     error
	message string
}

func (e schemaGuidanceError) Error() string {
	return e.message
}

func (e schemaGuidanceError) Unwrap() error {
	return e.err
}

func addSchemaFieldGuidance(err error, out any) error {
	if err == nil || out == nil {
		return err
	}
	fieldsByType, sectionByType := schemaVocabulary(reflect.TypeOf(out))
	message := unknownSchemaFieldPattern.ReplaceAllStringFunc(err.Error(), func(match string) string {
		parts := unknownSchemaFieldPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		suggestion := closestName(parts[1], fieldsByType[parts[2]])
		if suggestion == "" {
			return match
		}
		return match + ` (did you mean "` + suggestion + `"?)`
	})
	message = replaceSchemaTypeNames(message, sectionByType)
	message = unknownSchemaFieldPhrase.ReplaceAllString(message, "unknown field $1 in ")
	if message == err.Error() {
		return err
	}
	return schemaGuidanceError{err: err, message: message}
}

func schemaFieldsByType(root reflect.Type) map[string][]string {
	fieldsByType, _ := schemaVocabulary(root)
	return fieldsByType
}

func schemaVocabulary(root reflect.Type) (fieldsByType map[string][]string, sectionByType map[string]string) {
	fieldsByType = make(map[string][]string)
	sectionByType = make(map[string]string)
	visited := make(map[reflect.Type]bool)
	var visit func(reflect.Type, string)
	visit = func(t reflect.Type, section string) {
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
			t = t.Elem()
		}
		if t.Kind() == reflect.Map {
			visit(t.Elem(), section)
			return
		}
		if t.Kind() != reflect.Struct || visited[t] {
			return
		}
		visited[t] = true
		if section != "" {
			sectionByType[t.String()] = section
		}
		var fields []string
		for index := 0; index < t.NumField(); index++ {
			field := t.Field(index)
			tag := strings.Split(field.Tag.Get("yaml"), ",")[0]
			if field.PkgPath == "" && tag != "" && tag != "-" {
				fields = append(fields, tag)
			}
			visit(field.Type, tag)
		}
		sort.Strings(fields)
		fieldsByType[t.String()] = fields
	}
	visit(root, "")
	return fieldsByType, sectionByType
}

func replaceSchemaTypeNames(message string, sectionByType map[string]string) string {
	return schemaTypeNamePattern.ReplaceAllStringFunc(message, func(match string) string {
		parts := schemaTypeNamePattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		section := sectionByType[parts[1]]
		if section == "" {
			return match
		}
		return "a " + section + " entry"
	})
}

func closestName(requested string, available []string) string {
	best := ""
	bestDistance := len([]rune(requested)) + 1
	tied := false
	for _, candidate := range available {
		distance := suggest.Distance(
			strings.ToLower(requested),
			strings.ToLower(candidate),
		)
		switch {
		case distance < bestDistance:
			best = candidate
			bestDistance = distance
			tied = false
		case distance == bestDistance:
			tied = true
		}
	}
	threshold := (len([]rune(requested)) + 2) / 3
	if threshold < 1 {
		threshold = 1
	}
	if tied || bestDistance > threshold {
		return ""
	}
	return best
}

func schemaValueSuggestion(requested string, allowed ...string) string {
	suggestion := closestName(requested, allowed)
	if suggestion == "" {
		return ""
	}
	return ` (did you mean "` + suggestion + `"?)`
}
