package main

import (
	"strings"

	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

const (
	chatListWidth   = 26
	chatInputHeight = 3
	// header + status + the bordered bottom pane
	chatChromeHeight = 1 + 1 + chatInputHeight + 2
	// Below this the panes cannot be laid out at all.
	chatMinWidth  = 40
	chatMinHeight = 12
)

var (
	styleHeader     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleHeaderMeta = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleStatus     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleList       = lipgloss.NewStyle().
			Width(chatListWidth).
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(lipgloss.Color("240"))
	styleListItem     = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	styleListActive   = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	styleUser         = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	styleAssistant    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	styleTool         = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleSystem       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleBox          = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))
	styleApprovalBox  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("11"))
	styleApprovalHead = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	styleApprovalKeys = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
)

// View implements tea.Model.
func (m *chatModel) View() string {
	if !m.ready {
		return "dzzzr chat — запуск…"
	}
	if m.width < chatMinWidth || m.height < chatMinHeight {
		return "Окно терминала слишком мало для «dzzzr chat»"
	}
	bottom := m.bottomView()
	body := max(1, m.height-2-lipgloss.Height(bottom))
	return strings.Join([]string{
		m.headerView(),
		m.bodyView(body),
		m.statusView(),
		bottom,
	}, "\n")
}

func (m *chatModel) headerView() string {
	title := m.title
	if title == "" {
		title = "чат"
	}
	meta := []string{"город " + m.city, "права " + string(m.policy)}
	if m.agentCfg.Model != "" {
		meta = append(meta, m.agentCfg.Model)
	}
	line := styleHeader.Render("dzzzr chat · "+title) + " " + styleHeaderMeta.Render(strings.Join(meta, " · "))
	return fitWidth(line, m.width)
}

func (m *chatModel) statusView() string {
	parts := []string{"○ готов"}
	if m.running {
		parts = []string{"● агент работает (Esc — отмена)"}
	}
	if m.status != "" {
		parts = append(parts, m.status)
	}
	if m.note != "" {
		parts = append(parts, m.note)
	}
	return fitWidth(styleStatus.Render(strings.Join(parts, " · ")), m.width)
}

func (m *chatModel) bodyView(height int) string {
	m.view.Height = height
	transcript := m.view.View()
	if !m.showList {
		return transcript
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, m.listView(height), transcript)
}

func (m *chatModel) listView(height int) string {
	lines := make([]string, 0, height)
	for i, c := range m.chats {
		if len(lines) >= height {
			break
		}
		title := c.Title
		if title == "" {
			title = c.ID
		}
		if c.Running {
			title = "● " + title
		}
		marker, style := "  ", styleListItem
		if c.ID == m.chatID {
			marker, style = "▸ ", styleListActive
		}
		if m.focus == focusList && i == m.listIdx {
			marker = "> "
		}
		lines = append(lines, style.Render(fitWidth(marker+title, chatListWidth)))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return styleList.Height(height).Render(strings.Join(lines, "\n"))
}

func (m *chatModel) bottomView() string {
	width := max(10, m.width-2)
	if approval := m.currentApproval(); approval != nil {
		return styleApprovalBox.Width(width).Render(m.approvalView(*approval, width))
	}
	return styleBox.Width(width).Render(m.input.View())
}

func (m *chatModel) approvalView(approval approvalMsg, width int) string {
	head := "Агент хочет выполнить " + approval.tool
	// A run in another conversation can be the one asking, and approving a
	// code submission for the wrong chat is not a recoverable mistake.
	if approval.chatID != m.chatID {
		head += " — в чате «" + m.chatTitle(approval.chatID) + "»"
	}
	lines := []string{
		styleApprovalHead.Render(fitWidth(head, width)),
		fitWidth("  "+approval.args, width),
	}
	for len(lines) < chatInputHeight {
		lines = append(lines, "")
	}
	lines[chatInputHeight-1] = styleApprovalKeys.Render("[y] разрешить  [n] отклонить  [q] прервать ход")
	return strings.Join(lines[:chatInputHeight], "\n")
}

// renderTranscript builds the content of the scrolling pane.
func (m *chatModel) renderTranscript() string {
	width := max(10, m.view.Width-1)
	wrap := lipgloss.NewStyle().Width(width)
	var b strings.Builder
	for i, line := range m.transcript {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(roleLabel(line))
		b.WriteString("\n")
		if line.Role == chatRoleAssistant {
			// glamour wraps and colors the text itself; wrapping it again
			// would break its own escape sequences.
			b.WriteString(m.renderMarkdown(line.Content, width))
		} else {
			b.WriteString(wrap.Render(line.Content))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderMarkdown renders an answer for the terminal, falling back to the
// plain formatter when glamour cannot be built.
func (m *chatModel) renderMarkdown(content string, wrap int) string {
	r := m.markdownRenderer(wrap)
	if r == nil {
		return lipgloss.NewStyle().Width(wrap).Render(formatMarkdownForTerminal(content))
	}
	out, err := r.Render(content)
	if err != nil {
		return lipgloss.NewStyle().Width(wrap).Render(formatMarkdownForTerminal(content))
	}
	return strings.Trim(out, "\n")
}

// markdownRenderer builds the renderer once per wrap width, because glamour
// fixes the width when it is constructed.
func (m *chatModel) markdownRenderer(wrap int) *glamour.TermRenderer {
	wrap = max(wrap, 20)
	if m.renderer != nil && m.wrap == wrap {
		return m.renderer
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(chatMarkdownStyle()),
		glamour.WithWordWrap(wrap),
		glamour.WithEmoji(),
	)
	if err != nil {
		m.renderer, m.wrap = nil, 0
		return nil
	}
	m.renderer, m.wrap = r, wrap
	return r
}

// chatMarkdownStyle is glamour's dark theme without the document margin and
// the full-width background, so an answer lines up under its label and does
// not overflow the pane. Only the top-level pointers of the shared style are
// replaced, never what they point at.
func chatMarkdownStyle() glamouransi.StyleConfig {
	s := glamourstyles.DarkStyleConfig
	var zero uint
	s.Document.Margin = &zero
	s.Document.Color = nil
	s.Document.BlockPrefix = ""
	s.Document.BlockSuffix = ""
	return s
}

// chatTitle names a chat for a message about it, falling back to the
// identifier for a chat that has since been deleted.
func (m *chatModel) chatTitle(chatID string) string {
	for _, c := range m.chats {
		if c.ID == chatID && c.Title != "" {
			return c.Title
		}
	}
	return chatID
}

func roleLabel(line chatLine) string {
	switch line.Role {
	case chatRoleUser:
		return styleUser.Render("вы")
	case chatRoleAssistant:
		return styleAssistant.Render("агент")
	case chatRoleTool:
		name := line.ToolName
		if name == "" {
			name = "инструмент"
		}
		return styleTool.Render("· " + name)
	default:
		return styleSystem.Render("система")
	}
}

// fitWidth cuts a line to the space it has, counting what the terminal shows
// rather than the bytes or the escape sequences.
func fitWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}
