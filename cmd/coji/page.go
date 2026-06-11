package main

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss/tree"
	"github.com/mgilbir/coji/internal/core"
	"github.com/spf13/cobra"
)

func newPageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "page",
		Short: "Read, create, and edit pages",
	}
	cmd.AddCommand(newPageGetCmd())
	cmd.AddCommand(newPageCreateCmd())
	cmd.AddCommand(newPageEditCmd())
	cmd.AddCommand(newPageChildrenCmd())
	return cmd
}

func newPageChildrenCmd() *cobra.Command {
	var recursive, folder bool

	cmd := &cobra.Command{
		Use:   "children <id>",
		Short: "List the children of a page (or folder)",
		Long: `List the direct children of a page in the content tree, showing each
child's type and ID. Use --recursive for the full subtree, and --folder when
the ID is a folder rather than a page.

The IDs shown are what you pass to ` + "`page create --parent`" + ` to nest a new
page; --parent accepts a page or a folder ID.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := newService(cmd)
			if err != nil {
				return err
			}
			if recursive {
				root, err := svc.Tree(cmd.Context(), args[0], folder)
				if err != nil {
					return err
				}
				printTree(root)
				return nil
			}
			children, err := svc.Children(cmd.Context(), args[0], folder)
			if err != nil {
				return err
			}
			if len(children) == 0 {
				fmt.Println("No children.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "TYPE\tID\tTITLE")
			for _, ch := range children {
				fmt.Fprintf(w, "%s\t%s\t%s\n", ch.Type, ch.ID, ch.Title)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "show the full subtree")
	cmd.Flags().BoolVar(&folder, "folder", false, "treat the ID as a folder rather than a page")
	return cmd
}

// printTree renders a content tree using lipgloss's tree component. Each node
// shows its title followed by type and ID, so IDs are available for --parent.
func printTree(root *core.TreeNode) {
	fmt.Println(buildTree(root))
}

// buildTree converts a core.TreeNode into a lipgloss tree (root + nested
// subtrees), labelling every node uniformly.
func buildTree(n *core.TreeNode) *tree.Tree {
	t := tree.Root(nodeLabel(n))
	for _, c := range n.Children {
		t.Child(buildTree(c))
	}
	return t
}

func nodeLabel(n *core.TreeNode) string {
	title := n.Title
	if title == "" {
		title = "(root)"
	}
	return fmt.Sprintf("%s  [%s %s]", title, n.Type, n.ID)
}

func newPageGetCmd() *cobra.Command {
	var format, output string

	cmd := &cobra.Command{
		Use:   "get <page-id>",
		Short: "Fetch a page",
		Long:  "Fetch a page and print its body. Defaults to markdown; use --format storage|adf for the raw representation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := core.ParseFormat(format)
			if err != nil {
				return err
			}
			svc, err := newService(cmd)
			if err != nil {
				return err
			}
			p, err := svc.GetPage(cmd.Context(), args[0], f)
			if err != nil {
				return err
			}
			return writeOutput(output, p.Body)
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", "markdown", "body format: markdown, storage, or adf")
	cmd.Flags().StringVarP(&output, "output", "o", "-", "write body to a file (\"-\" for stdout)")
	return cmd
}

func newPageCreateCmd() *cobra.Command {
	var space, title, parent, format, input string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a page from markdown",
		Long:  "Create a page. Body is read from --input (a file, or \"-\" for stdin). Defaults to markdown.",
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := core.ParseFormat(format)
			if err != nil {
				return err
			}
			if title == "" {
				return fmt.Errorf("--title is required")
			}
			if space == "" {
				return fmt.Errorf("--space (a space key) is required")
			}
			content, err := readInput(input)
			if err != nil {
				return err
			}
			svc, err := newService(cmd)
			if err != nil {
				return err
			}
			p, err := svc.CreatePage(cmd.Context(), core.CreateInput{
				SpaceKey: space,
				Title:    title,
				ParentID: parent,
				Format:   f,
				Content:  content,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Created page %s (v%d): %s\n", p.ID, p.Version, p.Title)
			return nil
		},
	}
	cmd.Flags().StringVarP(&space, "space", "s", "", "space key (e.g. ENG)")
	cmd.Flags().StringVarP(&title, "title", "t", "", "page title")
	cmd.Flags().StringVar(&parent, "parent", "", "parent page ID")
	cmd.Flags().StringVarP(&format, "format", "f", "markdown", "body format: markdown, storage, or adf")
	cmd.Flags().StringVarP(&input, "input", "i", "-", "read body from a file (\"-\" for stdin)")
	return cmd
}

func newPageEditCmd() *cobra.Command {
	var title, format, input, message string

	cmd := &cobra.Command{
		Use:   "edit <page-id>",
		Short: "Edit an existing page from markdown",
		Long:  "Replace a page's body. Body is read from --input (a file, or \"-\" for stdin). The version is bumped automatically.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := core.ParseFormat(format)
			if err != nil {
				return err
			}
			content, err := readInput(input)
			if err != nil {
				return err
			}
			svc, err := newService(cmd)
			if err != nil {
				return err
			}
			p, err := svc.EditPage(cmd.Context(), core.EditInput{
				ID:         args[0],
				Title:      title,
				Format:     f,
				Content:    content,
				VersionMsg: message,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Updated page %s (v%d): %s\n", p.ID, p.Version, p.Title)
			return nil
		},
	}
	cmd.Flags().StringVarP(&title, "title", "t", "", "new title (keeps current if omitted)")
	cmd.Flags().StringVarP(&format, "format", "f", "markdown", "body format: markdown, storage, or adf")
	cmd.Flags().StringVarP(&input, "input", "i", "-", "read body from a file (\"-\" for stdin)")
	cmd.Flags().StringVarP(&message, "message", "m", "", "optional version/change message")
	return cmd
}

// readInput reads body content from a file path, or stdin when path is "-".
func readInput(path string) (string, error) {
	if path == "-" || path == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return string(data), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// writeOutput writes content to a file path, or stdout when path is "-".
func writeOutput(path, content string) error {
	if path == "-" || path == "" {
		_, err := fmt.Fprintln(os.Stdout, content)
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
