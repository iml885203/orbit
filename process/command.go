package process

import (
	"fmt"
	"os"

	"github.com/mattn/go-shellwords"
)

func commandArgs(command string, environment map[string]string) ([]string, error) {
	parser := shellwords.NewParser()
	parser.ParseEnv = true
	parser.Getenv = func(name string) string {
		if value, ok := environment[name]; ok {
			return value
		}
		return os.Getenv(name)
	}

	parts, err := parser.Parse(command)
	if err != nil {
		return nil, fmt.Errorf("parse command %q: %w", command, err)
	}
	return parts, nil
}

func commandEnvironment(overrides map[string]string) []string {
	environment := os.Environ()
	for name, value := range overrides {
		environment = append(environment, name+"="+value)
	}
	return environment
}
