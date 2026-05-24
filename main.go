package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	_ "github.com/mattn/go-sqlite3"
)

// docStyle defines the padding for our terminal view
var docStyle = lipgloss.NewStyle().Margin(1, 2)

// track represents a row from the Mixxx database library
type track struct {
	title  string
	artist string
	bpm    string
}

// Implement the list.Item interface so Bubble Tea can render it
func (t track) Title() string       { return t.title }
func (t track) Description() string { return fmt.Sprintf("Artist: %s | BPM: %s", t.artist, t.bpm) }
func (t track) FilterValue() string { return t.title + " " + t.artist }

type model struct {
	list list.Model
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string {
	return docStyle.Render(m.list.View())
}

// fetchTracks queries the local Mixxx SQLite database
func fetchTracks(dbPath string) ([]list.Item, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Mixxx stores library tracks in the 'library' table.
	// BPM is typically stored as a float; we format it cleanly or handle zeroes.
	query := `
		SELECT title, artist
		FROM library 
		WHERE mixxx_deleted = 0 
		LIMIT 200;
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []list.Item
	for rows.Next() {
		var t track
		if err := rows.Scan(&t.title, &t.artist, &t.bpm); err != nil {
			return nil, err
		}
		// Fallbacks for empty tags
		if t.title == "" {
			t.title = "Unknown Title"
		}
		if t.artist == "" {
			t.artist = "Unknown Artist"
		}
		items = append(items, t)
	}
	return items, nil
}

func main() {
	// Update this path to where you copied your mixxxdb.sqlite file
	dbPath := "./mixxxdb.sqlite"

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Fatalf("Error: Mixxx database not found at %s. Please copy your mixxxdb.sqlite here.", dbPath)
	}

	fmt.Println("Reading Mixxx database...")
	items, err := fetchTracks(dbPath)
	if err != nil {
		log.Fatalf("Failed to query database: %v", err)
	}

	// Set up the default Bubble Tea list model
	m := model{
		list: list.New(items, list.NewDefaultDelegate(), 0, 0),
	}
	m.list.Title = "Mixxx Library Browser"

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Alas, there's been an error: %v", err)
	}
}
