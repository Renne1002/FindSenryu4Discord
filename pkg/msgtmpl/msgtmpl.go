package msgtmpl

import (
	"fmt"
	"strings"

	"github.com/u16-io/FindSenryu4Discord/config"
)

// Get returns an overridden message template by key when configured,
// otherwise it returns fallback.
func Get(key, fallback string) string {
	conf, err := config.Load("config.toml")
	if err != nil {
		return fallback
	}
	if conf != nil && conf.Messages != nil {
		if v, ok := conf.Messages[key]; ok && v != "" {
			return v
		}
	}
	return fallback
}

// Format formats a configured template (or fallback) with fmt.Sprintf.
func Format(key, fallback string, args ...interface{}) string {
	return fmt.Sprintf(Get(key, fallback), args...)
}

// Fill replaces {name} placeholders in a configured template (or fallback).
func Fill(key, fallback string, vars map[string]string) string {
	t := Get(key, fallback)
	if len(vars) == 0 {
		return t
	}
	replacements := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		replacements = append(replacements, "{"+k+"}", v)
	}
	return strings.NewReplacer(replacements...).Replace(t)
}
