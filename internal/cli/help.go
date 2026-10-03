package cli

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/alecthomas/kong"
)

type HelpSchema struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Commands    []CommandSchema `json:"commands"`
	GlobalFlags []FlagSchema    `json:"global_flags"`
}

type CommandSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Flags       []FlagSchema    `json:"flags,omitempty"`
	Args        []ArgSchema     `json:"args,omitempty"`
	Subcommands []CommandSchema `json:"subcommands,omitempty"`
	Examples    []string        `json:"examples,omitempty"`
}

type FlagSchema struct {
	Name        string `json:"name"`
	Short       string `json:"short,omitempty"`
	Type        string `json:"type"`
	Default     string `json:"default,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description"`
}

type ArgSchema struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// GenerateHelpJSON derives the schema from the same Kong grammar used to parse
// commands, so new commands and flags are available to agents automatically.
func GenerateHelpJSON(cli *CLI) ([]byte, error) {
	parser, err := kong.New(cli, kong.Name("pm-cli"))
	if err != nil {
		return nil, err
	}
	schema := HelpSchema{Name: "pm-cli", Version: Version,
		Description: "ProtonMail CLI via Proton Bridge IMAP/SMTP",
		Commands:    commandSchemas(parser.Model.Node, ""),
		GlobalFlags: flagSchemas(parser.Model.Flags),
	}
	return json.MarshalIndent(schema, "", "  ")
}

func commandSchemas(node *kong.Node, prefix string) []CommandSchema {
	var result []CommandSchema
	for _, child := range node.Children {
		if child.Type != kong.CommandNode || child.Hidden {
			continue
		}
		name := strings.TrimSpace(prefix + " " + child.Name)
		command := CommandSchema{Name: name, Description: child.Help,
			Flags: flagSchemas(child.Flags), Subcommands: commandSchemas(child, name)}
		for _, arg := range child.Positional {
			command.Args = append(command.Args, ArgSchema{Name: arg.Name,
				Type: getTypeString(arg.Target.Type()), Required: arg.Required,
				Description: arg.Help})
		}
		result = append(result, command)
	}
	return result
}

func flagSchemas(flags []*kong.Flag) []FlagSchema {
	var result []FlagSchema
	for _, flag := range flags {
		if flag.Hidden || flag.Name == "help" {
			continue
		}
		short := ""
		if flag.Short != 0 {
			short = "-" + string(flag.Short)
		}
		result = append(result, FlagSchema{Name: "--" + flag.Name, Short: short,
			Type: getTypeString(flag.Target.Type()), Default: flag.Default,
			Required: flag.Required, Description: flag.Help})
	}
	return result
}

func getTypeString(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "int"
	case reflect.Bool:
		return "bool"
	case reflect.Slice:
		return "[]" + getTypeString(t.Elem())
	case reflect.Map:
		return "map[string]string"
	default:
		return t.String()
	}
}

func PrintHelpJSON(cli *CLI) error {
	data, err := GenerateHelpJSON(cli)
	if err != nil {
		return fmt.Errorf("failed to generate help JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}
