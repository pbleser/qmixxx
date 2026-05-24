package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	_ "github.com/mattn/go-sqlite3"
)

var (
	docStyle       = lipgloss.NewStyle().Margin(1, 2)
	highlightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true) // Bright Pink/Magenta for matches
	mutedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	dbConn         *sql.DB
)

type track struct {
	title     string
	artist    string
	genre     string
	playCount int
	rawQuery  string // Kept so the renderer knows what terms to highlight
}

func (t track) Title() string       { return t.title }
func (t track) Description() string { return t.artist }
func (t track) FilterValue() string { return "" }

// --- CUSTOM DELEGATE FOR HIGHLIGHTED RENDERING ---
type trackDelegate struct{}

func (d trackDelegate) Height() int                               { return 2 }
func (d trackDelegate) Spacing() int                              { return 1 }
func (d trackDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }

func (d trackDelegate) Render(w io.Writer, l list.Model, index int, item list.Item) {
	t, ok := item.(track)
	if !ok {
		return
	}

	// Tokenize current query terms for the highlighter
	searchWords := strings.Fields(strings.ToLower(t.rawQuery))

	titleText := highlightMatches(t.title, searchWords)
	artistText := highlightMatches(t.artist, searchWords)
	genreText := highlightMatches(mFallback(t.genre, "No Genre"), searchWords)
	playsText := mutedStyle.Render(fmt.Sprintf("| Plays: %d", t.playCount))

	// Highlight current active keyboard-selected line item
	isSelected := index == l.Index()
	if isSelected {
		titleText = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true).Render("> " + t.title)
		artistText = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Render(t.artist)
		genreText = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(mFallback(t.genre, "No Genre"))
	}

	fmt.Fprintf(w, "%s\n   %s   %s %s",
		titleText,
		artistText,
		mutedStyle.Render("• Genre: "+genreText),
		playsText,
	)
}

// --- CORE HIGHLIGHTING LOGIC ---
func highlightMatches(src string, words []string) string {
	if len(words) == 0 || src == "" {
		return src
	}

	// Escape search terms to safely create a regex pattern
	var escapedWords []string
	for _, w := range words {
		if trimmed := strings.TrimSpace(w); trimmed != "" {
			escapedWords = append(escapedWords, regexp.QuoteMeta(trimmed))
		}
	}

	if len(escapedWords) == 0 {
		return src
	}

	// Match any of the target search terms, case-insensitive
	pattern := "(?i)(" + strings.Join(escapedWords, "|") + ")"
	re, err := regexp.Compile(pattern)
	if err != nil {
		return src
	}

	// Replace matched substrings with our custom Lip Gloss terminal escape styles
	return re.ReplaceAllStringFunc(src, func(match string) string {
		return highlightStyle.Render(match)
	})
}

// --- BUBBLE TEA MODEL ---
type model struct {
	list      list.Model
	textInput textinput.Model
	searching bool
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Type to search tracks (Artist, Title, Genre)..."
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 40

	// Use our custom trackDelegate instead of list.NewDefaultDelegate()
	l := list.New([]list.Item{}, trackDelegate{}, 0, 0)
	l.Title = "Mixxx Library Browser"
	l.SetShowFilter(false)

	return model{
		list:      l,
		textInput: ti,
		searching: true,
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit

		case "enter":
			if m.searching {
				// 1. Existing Search Submission Logic
				searchTerm := m.textInput.Value()
				items, err := queryTracksFromDB(searchTerm)
				if err != nil {
					m.list.Title = fmt.Sprintf("Error query: %v", err)
				} else {
					m.list.SetItems(items)
					m.list.Title = fmt.Sprintf("Results for '%s' (%d found)", searchTerm, len(items))
				}
				m.searching = false
				m.textInput.Blur()
			} else {
				// 2. NEW: List Selection Logic (Copy to Clipboard)
				if selectedItem := m.list.SelectedItem(); selectedItem != nil {
					if t, ok := selectedItem.(track); ok {
						// Format how you want it copied, e.g., "Artist - Title"
						clipboardText := fmt.Sprintf("%s - %s", t.artist, t.title)

						err := clipboard.WriteAll(clipboardText)
						if err != nil {
							m.list.Title = fmt.Sprintf("Clipboard Error: %v", err)
						} else {
							m.list.Title = fmt.Sprintf("Copied: %s", clipboardText)
						}
					}
				}
			}

		case "/":
			if !m.searching {
				m.searching = true
				m.textInput.Focus()
				m.textInput.SetValue("")
				return m, textinput.Blink
			}
		}

	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v-4)
	}

	// Route keystrokes properly based on focus
	if m.searching {
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	var s strings.Builder
	s.WriteString(m.textInput.View())
	s.WriteString("\n\n")

	if m.searching {
		s.WriteString(mutedStyle.Render(" Press [Enter] to query database."))
		s.WriteString("\n\n")
	} else {
		s.WriteString(mutedStyle.Render(" Press [/] to search again | Arrow keys to navigate."))
		s.WriteString("\n\n")
	}

	s.WriteString(m.list.View())
	return docStyle.Render(s.String())
}

func queryTracksFromDB(filter string) ([]list.Item, error) {
	trimmed := strings.TrimSpace(filter)
	if trimmed == "" {
		return []list.Item{}, nil
	}
	words := strings.Fields(trimmed)

	var conditions []string
	var queryArgs []any

	for range words {
		conditions = append(conditions, "(artist LIKE ? OR title LIKE ? OR genre LIKE ?)")
	}
	whereClause := strings.Join(conditions, " AND ")

	for _, word := range words {
		pattern := "%" + word + "%"
		queryArgs = append(queryArgs, pattern, pattern, pattern)
	}

	baseQuery := fmt.Sprintf(`
		SELECT title, artist, IFNULL(genre, ''), IFNULL(timesplayed, 0)
		FROM library 
		WHERE mixxx_deleted = 0 
		  AND %s
		ORDER BY timesplayed DESC
		LIMIT 200;
	`, whereClause)

	rows, err := dbConn.Query(baseQuery, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []list.Item
	for rows.Next() {
		var t track
		if err := rows.Scan(&t.title, &t.artist, &t.genre, &t.playCount); err != nil {
			return nil, err
		}
		t.title = mFallback(t.title, "Unknown Title")
		t.artist = mFallback(t.artist, "Unknown Artist")
		t.rawQuery = filter // Store the raw search string into the item for visual parsing
		items = append(items, t)
	}
	return items, nil
}

func mFallback(val, fallback string) string {
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Error: must specify path to Mixxx database.")
	}
	dbPath := os.Args[1]

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Fatalf("Error: Mixxx database not found at %s.", dbPath)
	}

	var err error
	dbConn, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbConn.Close()

	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Runtime error: %v", err)
	}
}
