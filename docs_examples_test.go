package main

import "testing"

// TestEveryCommandHasExamples: toda orden lleva ejemplos en docs/COMMANDS.md
// (docs_examples.go); una orden nueva sin ellos falla aquí.
func TestEveryCommandHasExamples(t *testing.T) {
	for name := range commands {
		if len(commandExamples[name]) == 0 {
			t.Errorf("/%s no tiene ejemplos en docs_examples.go", name)
		}
	}
	for name := range commandExamples {
		if _, ok := commands[name]; !ok {
			t.Errorf("ejemplos de /%s, que no es una orden", name)
		}
	}
}
