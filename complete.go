package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// completeModel is omp's fast-model role: low latency for keystroke-driven completions.
const completeModel = "pi/smol"

// Completer asks the local omp agent, in print mode, for short completions.
type Completer struct {
	Bin     string
	Model   string
	Dir     string
	Context string
}

const completeSystemPrompt = `You autocomplete task descriptions that a developer is typing for an AI coding agent that will work in their repository.
Return the draft followed by a natural continuation: finish the current sentence, plus at most one short sentence, so the task becomes concrete and actionable.
Copy the draft exactly, character for character, before your continuation.
Output only the completed text: no quotes, no markdown, no commentary.`

const branchSystemPrompt = `Generate a git branch name for the task below.
Rules: kebab-case, lowercase letters, digits and hyphens only; 1-3 words; at most 24 characters; name the thing being changed; no type prefixes such as feat/ or fix/.
Output only the branch name, nothing else.`

func (c *Completer) ask(ctx context.Context, system, prompt string) (string, error) {
	args := []string{
		"-p", "--no-session", "--no-tools", "--no-skills", "--no-rules", "--no-context-files",
		"--no-extensions", "--no-lsp", "--no-title", "--thinking", "off",
		"--system-prompt", system,
	}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	cmd := exec.CommandContext(ctx, c.Bin, append(args, prompt)...)
	cmd.Dir = c.Dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		return "", fmt.Errorf("omp: %v: %s", err, msg)
	}
	return string(out), nil
}

// Complete returns the text to append to draft.
func (c *Completer) Complete(ctx context.Context, draft string) (string, error) {
	prompt := fmt.Sprintf("<repository>\n%s</repository>\n\n<draft>%s</draft>", c.Context, draft)
	out, err := c.ask(ctx, completeSystemPrompt, prompt)
	if err != nil {
		return "", err
	}
	return continuation(draft, out), nil
}

// BranchName asks the model for a branch name for task.
func (c *Completer) BranchName(ctx context.Context, task string) (string, error) {
	out, err := c.ask(ctx, branchSystemPrompt, task)
	if err != nil {
		return "", err
	}
	return slugify(out), nil
}

const maxSuggestion = 240

// continuation extracts what the model added after draft. The model is asked
// to echo the draft so word joins are unambiguous; output that does not echo
// it is treated as a bare continuation, and output that rewrites the draft is
// rejected.
func continuation(draft, out string) string {
	out = strings.TrimSpace(out)
	out = strings.TrimPrefix(out, "<draft>")
	out = strings.TrimSuffix(out, "</draft>")
	if len(out) >= 2 && strings.ContainsRune(`"'`+"`", rune(out[0])) && out[len(out)-1] == out[0] {
		out = out[1 : len(out)-1]
	}
	head := strings.TrimRightFunc(draft, unicode.IsSpace)
	draftEndsInSpace := len(head) < len(draft)
	var rest string
	switch {
	case strings.HasPrefix(out, head):
		rest = out[len(head):] // keeps the model's join: " persists" vs "tor" after "refac"
		if draftEndsInSpace {
			rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		}
	case strings.EqualFold(firstWord(out), firstWord(head)):
		return "" // the model rewrote the draft instead of continuing it
	default:
		rest = strings.TrimLeftFunc(out, unicode.IsSpace)
		if rest != "" && !draftEndsInSpace && !strings.ContainsRune(".,;:!?)", rune(rest[0])) {
			rest = " " + rest
		}
	}

	body := strings.Join(strings.Fields(rest), " ") // single line, like typed text
	if body == "" {
		return ""
	}
	if len(body) > maxSuggestion {
		cut := strings.LastIndexByte(body[:maxSuggestion], ' ')
		if cut <= 0 {
			cut = maxSuggestion
			for cut > 0 && !utf8.RuneStart(body[cut]) {
				cut--
			}
		}
		body = body[:cut]
	}
	if unicode.IsSpace(rune(rest[0])) {
		body = " " + body
	}
	return body
}

func firstWord(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slugify turns text into a short kebab-case branch name.
func slugify(s string) string {
	words := strings.Fields(nonSlug.ReplaceAllString(strings.ToLower(s), " "))
	var b strings.Builder
	for _, w := range words {
		if b.Len()+len(w)+1 > 24 && b.Len() > 0 {
			break
		}
		if b.Len() > 0 {
			b.WriteByte('-')
		}
		b.WriteString(w)
	}
	return strings.Trim(b.String()[:min(b.Len(), 24)], "-")
}
