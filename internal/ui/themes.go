package ui

import "charm.land/lipgloss/v2"

// Theme is a color palette (hex values).
type Theme struct {
	Name      string
	Dark      bool
	Accent    string
	Secondary string
	Muted     string
	Success   string
	Warning   string
	Err       string
	White     string
}

// BuiltinThemes lists every shipped theme in display order.
var BuiltinThemes = []Theme{
	{
		Name:      "dark",
		Dark:      true,
		Accent:    "#00D7FF",
		Secondary: "#C3E88D",
		Muted:     "#6C7086",
		Success:   "#A6E3A1",
		Warning:   "#F9E2AF",
		Err:       "#F38BA8",
		White:     "#CDD6F4",
	},
	{
		Name:      "dracula",
		Dark:      true,
		Accent:    "#BD93F9",
		Secondary: "#FF79C6",
		Muted:     "#6272A4",
		Success:   "#50FA7B",
		Warning:   "#F1FA8C",
		Err:       "#FF5555",
		White:     "#F8F8F2",
	},
	{
		Name:      "gruvbox",
		Dark:      true,
		Accent:    "#FABD2F",
		Secondary: "#B8BB26",
		Muted:     "#928374",
		Success:   "#B8BB26",
		Warning:   "#FE8019",
		Err:       "#FB4934",
		White:     "#EBDBB2",
	},
	{
		Name:      "light",
		Dark:      false,
		Accent:    "#0077AA",
		Secondary: "#2A6E2A",
		Muted:     "#555570",
		Success:   "#1E6B1E",
		Warning:   "#7A5500",
		Err:       "#AA2020",
		White:     "#0D0D20",
	},
}

// ThemeByName looks up a built-in theme by name.
func ThemeByName(name string) (Theme, bool) {
	for _, t := range BuiltinThemes {
		if t.Name == name {
			return t, true
		}
	}
	return Theme{}, false
}

// ApplyTheme updates every global style variable with the theme's colors.
func ApplyTheme(t Theme) {
	Accent = lipgloss.Color(t.Accent)
	Secondary = lipgloss.Color(t.Secondary)
	Muted = lipgloss.Color(t.Muted)
	Success = lipgloss.Color(t.Success)
	Warning = lipgloss.Color(t.Warning)
	Err = lipgloss.Color(t.Err)
	White = lipgloss.Color(t.White)
	rebuildStyles()
}

// InitTheme applies the named theme; falls back to the auto-detected one if the name is empty or unknown.
func InitTheme(name string) {
	if t, ok := ThemeByName(name); ok {
		ApplyTheme(t)
	}
	// "auto" or unknown: keep the palette set by styles.go init().
}
