package common

import "github.com/charmbracelet/lipgloss"

// Theme defines a set of colors for the UI.
type Theme struct {
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color
	Danger    lipgloss.Color
	Muted     lipgloss.Color
	Warning   lipgloss.Color
	Info      lipgloss.Color
	White     lipgloss.Color
	InputBg   lipgloss.Color
}

// BuiltinThemes maps theme names to their color definitions.
var BuiltinThemes = map[string]Theme{
	"default": {
		Primary:   "#7C3AED",
		Secondary: "#6B7280",
		Accent:    "#10B981",
		Danger:    "#EF4444",
		Muted:     "#9CA3AF",
		Warning:   "#F59E0B",
		Info:      "#EC4899",
		White:     "#FFFFFF",
		InputBg:   "#374151",
	},
	"solarized": {
		Primary:   "#268BD2",
		Secondary: "#586E75",
		Accent:    "#859900",
		Danger:    "#DC322F",
		Muted:     "#839496",
		Warning:   "#B58900",
		Info:      "#D33682",
		White:     "#FDF6E3",
		InputBg:   "#073642",
	},
	"nord": {
		Primary:   "#88C0D0",
		Secondary: "#4C566A",
		Accent:    "#A3BE8C",
		Danger:    "#BF616A",
		Muted:     "#D8DEE9",
		Warning:   "#EBCB8B",
		Info:      "#B48EAD",
		White:     "#ECEFF4",
		InputBg:   "#3B4252",
	},
	"gruvbox": {
		Primary:   "#D79921",
		Secondary: "#928374",
		Accent:    "#B8BB26",
		Danger:    "#FB4934",
		Muted:     "#A89984",
		Warning:   "#FE8019",
		Info:      "#D3869B",
		White:     "#FBF1C7",
		InputBg:   "#3C3836",
	},
}

// --- Colors (single source of truth) ---
var (
	Primary   = lipgloss.Color("#7C3AED") // Purple — active elements, cursor
	Secondary = lipgloss.Color("#6B7280") // Gray — borders, inactive tabs
	Accent    = lipgloss.Color("#10B981") // Green — synced status
	Danger    = lipgloss.Color("#EF4444") // Red — errors
	Muted     = lipgloss.Color("#9CA3AF") // Light gray — read messages, hints
	Warning   = lipgloss.Color("#F59E0B") // Amber — syncing
	Info      = lipgloss.Color("#EC4899") // Pink — selected items
	White     = lipgloss.Color("#FFFFFF")
	InputBg   = lipgloss.Color("#374151") // Dark gray — search input background
)

// ApplyTheme sets the global color variables to a named theme's colors.
func ApplyTheme(name string) {
	t, ok := BuiltinThemes[name]
	if !ok {
		return
	}
	Primary = t.Primary
	Secondary = t.Secondary
	Accent = t.Accent
	Danger = t.Danger
	Muted = t.Muted
	Warning = t.Warning
	Info = t.Info
	White = t.White
	InputBg = t.InputBg

	// Rebuild styles with new colors
	ActiveTab = lipgloss.NewStyle().Bold(true).Foreground(White).Background(Primary).Padding(0, 2)
	InactiveTab = lipgloss.NewStyle().Foreground(Secondary).Padding(0, 2)
	TabBar = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).BorderForeground(Secondary)
	SelectedMessage = lipgloss.NewStyle().Bold(true).Foreground(White).Background(Primary).Padding(0, 1)
	CheckedMessage = lipgloss.NewStyle().Foreground(Info).Bold(true)
	ReadMessage = lipgloss.NewStyle().Foreground(Muted).Padding(0, 1)
	StatusBar = lipgloss.NewStyle().Foreground(Muted).BorderStyle(lipgloss.NormalBorder()).BorderTop(true).BorderForeground(Secondary).Padding(0, 1)
	ErrorStyle = lipgloss.NewStyle().Foreground(Danger).Bold(true)
	Title = lipgloss.NewStyle().Bold(true).Foreground(Primary)
	SyncingStyle = lipgloss.NewStyle().Foreground(Warning).Italic(true)
	SyncedStyle = lipgloss.NewStyle().Foreground(Accent)
	MutedStyle = lipgloss.NewStyle().Foreground(Muted)
	SearchInputStyle = lipgloss.NewStyle().Foreground(White).Background(InputBg).Padding(0, 1)
}

// --- Styles ---
var (
	// Tabs
	ActiveTab   = lipgloss.NewStyle().Bold(true).Foreground(White).Background(Primary).Padding(0, 2)
	InactiveTab = lipgloss.NewStyle().Foreground(Secondary).Padding(0, 2)
	TabBar      = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).BorderForeground(Secondary)

	// Message list
	SelectedMessage = lipgloss.NewStyle().Bold(true).Foreground(White).Background(Primary).Padding(0, 1)
	CheckedMessage  = lipgloss.NewStyle().Foreground(Info).Bold(true)
	UnreadMessage   = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	ReadMessage     = lipgloss.NewStyle().Foreground(Muted).Padding(0, 1)

	// Chrome
	StatusBar    = lipgloss.NewStyle().Foreground(Muted).BorderStyle(lipgloss.NormalBorder()).BorderTop(true).BorderForeground(Secondary).Padding(0, 1)
	ReaderHeader = lipgloss.NewStyle().Bold(true)
	ErrorStyle   = lipgloss.NewStyle().Foreground(Danger).Bold(true)
	Title        = lipgloss.NewStyle().Bold(true).Foreground(Primary)

	// Status indicators
	SyncingStyle = lipgloss.NewStyle().Foreground(Warning).Italic(true)
	SyncedStyle  = lipgloss.NewStyle().Foreground(Accent)
	MutedStyle   = lipgloss.NewStyle().Foreground(Muted)

	// Search input
	SearchInputStyle = lipgloss.NewStyle().Foreground(White).Background(InputBg).Padding(0, 1)

	// Inline link references in reader
	LinkRefStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Italic(true) // Sky blue
)
