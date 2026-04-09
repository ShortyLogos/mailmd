package common

import "github.com/charmbracelet/lipgloss"

// --- Colors (single source of truth) ---
var (
	Primary   = lipgloss.Color("#7C3AED") // Purple — active elements, cursor
	Secondary = lipgloss.Color("#6B7280") // Gray — borders, inactive tabs
	Accent    = lipgloss.Color("#10B981") // Green — synced status
	Danger    = lipgloss.Color("#EF4444") // Red — errors
	Muted     = lipgloss.Color("#9CA3AF") // Light gray — read messages, hints
	Warning   = lipgloss.Color("#F59E0B") // Amber — syncing
	Info      = lipgloss.Color("#06B6D4") // Cyan — selected items
	White     = lipgloss.Color("#FFFFFF")
	InputBg   = lipgloss.Color("#374151") // Dark gray — search input background
)

// --- Styles ---
var (
	// Tabs
	ActiveTab   = lipgloss.NewStyle().Bold(true).Foreground(Primary).Padding(0, 2)
	InactiveTab = lipgloss.NewStyle().Foreground(Secondary).Padding(0, 2)
	TabBar      = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).BorderForeground(Secondary)

	// Message list
	SelectedMessage = lipgloss.NewStyle().Bold(true).Foreground(White).Background(Primary).Padding(0, 1)
	CheckedMessage  = lipgloss.NewStyle().Foreground(Info).Bold(true)
	UnreadMessage   = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	ReadMessage     = lipgloss.NewStyle().Foreground(Muted).Padding(0, 1)

	// Chrome
	StatusBar    = lipgloss.NewStyle().Foreground(Muted).BorderStyle(lipgloss.NormalBorder()).BorderTop(true).BorderForeground(Secondary).Padding(0, 1)
	ReaderHeader = lipgloss.NewStyle().Bold(true).Padding(0, 0, 1, 0)
	ErrorStyle   = lipgloss.NewStyle().Foreground(Danger).Bold(true)
	Title        = lipgloss.NewStyle().Bold(true).Foreground(Primary)

	// Status indicators
	SyncingStyle = lipgloss.NewStyle().Foreground(Warning).Italic(true)
	SyncedStyle  = lipgloss.NewStyle().Foreground(Accent)
	MutedStyle   = lipgloss.NewStyle().Foreground(Muted)

	// Search input
	SearchInputStyle = lipgloss.NewStyle().Foreground(White).Background(InputBg).Padding(0, 1)
)
