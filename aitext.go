package main

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

const (
	completeDebounce = 600 * time.Millisecond
	completeTimeout  = 30 * time.Second
	minDraftLen      = 10
)

// AIText is a huh Text field with AI autocomplete: after a typing pause it
// asks the Completer for a continuation, shows it as ghost text below the box,
// and Tab inserts it.
type AIText struct {
	*huh.Text

	value     *string
	completer *Completer
	theme     huh.Theme
	dark      bool
	width     int
	focused   bool

	seq        int // bumped on every value change; stale results are dropped
	requested  int // seq of the last started request (group delivers messages twice)
	cancel     context.CancelFunc
	loading    bool
	suggestion string
	err        error
	accept     key.Binding
}

type debounceMsg struct {
	field *AIText
	seq   int
}

type suggestionMsg struct {
	field *AIText
	seq   int
	text  string
	err   error
}

// NewAIText binds a text field to value. A nil completer disables autocomplete.
func NewAIText(value *string, completer *Completer) *AIText {
	return &AIText{
		Text:      huh.NewText().Value(value),
		value:     value,
		completer: completer,
		theme:     huh.ThemeFunc(huh.ThemeCharm),
		dark:      true,
		accept:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "accept suggestion")),
	}
}

func (t *AIText) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		t.dark = msg.IsDark()
	case debounceMsg:
		if msg.field == t && msg.seq == t.seq && msg.seq != t.requested && t.focused {
			return t, t.request()
		}
		return t, nil
	case suggestionMsg:
		if msg.field == t && msg.seq == t.seq {
			t.loading = false
			t.suggestion, t.err = msg.text, msg.err
		}
		return t, nil
	case tea.KeyPressMsg:
		if t.suggestion != "" && key.Matches(msg, t.accept) {
			return t, t.acceptSuggestion()
		}
	}

	before := *t.value
	_, cmd := t.Text.Update(msg)
	if *t.value != before {
		return t, tea.Batch(cmd, t.changed())
	}
	return t, cmd
}

// acceptSuggestion moves the cursor to the end and pastes the suggestion.
func (t *AIText) acceptSuggestion() tea.Cmd {
	s := t.suggestion
	t.Text.Update(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	_, cmd := t.Text.Update(tea.PasteMsg{Content: s})
	return tea.Batch(cmd, t.changed())
}

// changed drops the current suggestion and schedules a new request.
func (t *AIText) changed() tea.Cmd {
	t.seq++
	t.reset()
	if t.completer == nil || len(strings.TrimSpace(*t.value)) < minDraftLen {
		return nil
	}
	field, seq := t, t.seq
	return tea.Tick(completeDebounce, func(time.Time) tea.Msg { return debounceMsg{field, seq} })
}

func (t *AIText) reset() {
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	t.loading, t.suggestion, t.err = false, "", nil
}

func (t *AIText) request() tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), completeTimeout)
	t.requested, t.cancel, t.loading = t.seq, cancel, true
	field, seq, draft, c := t, t.seq, *t.value, t.completer
	return func() tea.Msg {
		defer cancel()
		text, err := c.Complete(ctx, draft)
		return suggestionMsg{field: field, seq: seq, text: text, err: err}
	}
}

func (t *AIText) Focus() tea.Cmd {
	t.focused = true
	return t.Text.Focus()
}

func (t *AIText) Blur() tea.Cmd {
	t.focused = false
	t.seq++
	t.reset()
	return t.Text.Blur()
}

func (t *AIText) KeyBinds() []key.Binding {
	binds := t.Text.KeyBinds()
	if t.suggestion != "" {
		binds = append([]key.Binding{t.accept}, binds...)
	}
	return binds
}

func (t *AIText) WithTheme(theme huh.Theme) huh.Field {
	t.theme = theme
	t.Text.WithTheme(theme)
	return t
}

func (t *AIText) WithWidth(width int) huh.Field {
	t.width = width
	t.Text.WithWidth(width)
	return t
}

func (t *AIText) WithKeyMap(k *huh.KeyMap) huh.Field {
	t.Text.WithKeyMap(k)
	return t
}

func (t *AIText) WithHeight(height int) huh.Field {
	t.Text.WithHeight(height)
	return t
}

func (t *AIText) WithPosition(p huh.FieldPosition) huh.Field {
	t.Text.WithPosition(p)
	return t
}

func (t *AIText) View() string {
	view := t.Text.View()
	if !t.focused || t.completer == nil {
		return view
	}
	st := t.theme.Theme(t.dark).Focused
	ghost := st.TextInput.Placeholder
	var line string
	switch {
	case t.suggestion != "":
		line = st.TextInput.Text.Render(lastWords(*t.value, 4)) + ghost.Italic(true).Render(t.suggestion) +
			"  " + st.Description.Render("⇥ tab")
	case t.loading:
		line = ghost.Render("✦ thinking…")
	case t.err != nil:
		line = ghost.Render("✦ autocomplete unavailable: " + t.err.Error())
	default:
		line = ghost.Render("✦ AI autocomplete: pause typing for a suggestion")
	}
	if w := t.width - st.Base.GetHorizontalFrameSize(); w > 0 {
		line = lipgloss.NewStyle().Width(w).Render(line)
	}
	return lipgloss.JoinVertical(lipgloss.Left, view, st.Base.Render(line))
}

// lastWords returns the tail of s (up to n words) prefixed with an ellipsis,
// so the ghost text reads as a continuation of what was typed.
func lastWords(s string, n int) string {
	fields := strings.Fields(s)
	tail := strings.Join(fields[max(0, len(fields)-n):], " ")
	if len(fields) > n {
		tail = "…" + tail
	}
	if strings.HasSuffix(s, " ") || strings.HasSuffix(s, "\n") {
		tail += " "
	}
	return tail
}
