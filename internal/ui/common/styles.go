package common

import "github.com/charmbracelet/lipgloss"

var (
	Primary   = lipgloss.Color("#7C3AED")
	Secondary = lipgloss.Color("#6B7280")
	Accent    = lipgloss.Color("#10B981")
	Danger    = lipgloss.Color("#EF4444")
	Muted     = lipgloss.Color("#9CA3AF")

	ActiveTab   = lipgloss.NewStyle().Bold(true).Foreground(Primary).Padding(0, 2)
	InactiveTab = lipgloss.NewStyle().Foreground(Secondary).Padding(0, 2)
	TabBar      = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).BorderForeground(Secondary)

	SelectedMessage = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(Primary).Padding(0, 1)
	UnreadMessage   = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	ReadMessage     = lipgloss.NewStyle().Foreground(Muted).Padding(0, 1)

	StatusBar    = lipgloss.NewStyle().Foreground(Muted).BorderStyle(lipgloss.NormalBorder()).BorderTop(true).BorderForeground(Secondary).Padding(0, 1)
	ReaderHeader = lipgloss.NewStyle().Bold(true).Padding(0, 0, 1, 0)
	ErrorStyle   = lipgloss.NewStyle().Foreground(Danger).Bold(true)
	Title        = lipgloss.NewStyle().Bold(true).Foreground(Primary)
)
