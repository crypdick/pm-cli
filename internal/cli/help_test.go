package cli

import (
	"encoding/json"
	"testing"
)

func TestHelpSchemaIncludesEveryCommand(t *testing.T) {
	data, err := GenerateHelpJSON(&CLI{})
	if err != nil {
		t.Fatal(err)
	}
	var schema HelpSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	commands := map[string]CommandSchema{}
	var visit func([]CommandSchema)
	visit = func(nodes []CommandSchema) {
		for _, n := range nodes {
			commands[n.Name] = n
			visit(n.Subcommands)
		}
	}
	visit(schema.Commands)
	for _, name := range []string{"mail reply", "mail forward", "mail watch", "mail batch", "mail draft edit", "contacts add", "config doctor"} {
		if _, ok := commands[name]; !ok {
			t.Errorf("missing command %s", name)
		}
	}
	flags := map[string]FlagSchema{}
	for _, f := range commands["mail send"].Flags {
		flags[f.Name] = f
	}
	for _, name := range []string{"--idempotency-key", "--template", "--vars"} {
		if _, ok := flags[name]; !ok {
			t.Errorf("missing send flag %s", name)
		}
	}
	if flags["--to"].Required {
		t.Error("send --to is optional when using a template")
	}
}
