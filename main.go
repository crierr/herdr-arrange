// Command arrange is the herdr-arrange plugin: an interactive popup UI for
// moving, swapping, re-splitting and laying out herdr panes.
//
// One binary, several roles, chosen by the first argument:
//
//	arrange open        [[actions]]  open the popup, layout mode if the tab has >1 pane
//	arrange open-tree   [[actions]]  open the popup in tree-view mode
//	arrange ui          [[panes]]    the popup UI itself
//	arrange drain       [[startup]]  recover panes stranded by an interrupted rebuild
//	arrange call M [P]               debug: send one raw API call and print the result
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/crierr/herdr-arrange/internal/herdr"
)

const usage = `arrange — interactive pane move, swap, re-split and layout

usage:
  arrange open              open the arrange popup
  arrange open-tree         open the popup in tree-view mode
  arrange ui                run the popup UI (invoked by herdr)
  arrange drain             recover panes from an interrupted rebuild
  arrange call M [params]   send one raw API call and print the result
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch cmd := os.Args[1]; cmd {
	case "call":
		err = runCall(os.Args[2:])
	case "open", "open-tree", "ui", "drain":
		err = fmt.Errorf("%s: not implemented yet", cmd)
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "arrange: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "arrange: %v\n", err)
		os.Exit(1)
	}
}

// runCall is a development aid: it sends one arbitrary method with JSON params
// and pretty-prints the result, so the API can be poked without a UI.
func runCall(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("call: want a method name")
	}
	client, err := herdr.New()
	if err != nil {
		return err
	}

	var params any
	if len(args) > 1 && args[1] != "" {
		if err := json.Unmarshal([]byte(args[1]), &params); err != nil {
			return fmt.Errorf("call: params is not valid JSON: %w", err)
		}
	}

	var result json.RawMessage
	if err := client.Call(context.Background(), args[0], params, &result); err != nil {
		return err
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, result, "", "  "); err != nil {
		fmt.Println(string(result))
		return nil
	}
	fmt.Println(pretty.String())
	return nil
}
