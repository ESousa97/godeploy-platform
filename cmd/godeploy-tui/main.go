package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	_ "modernc.org/sqlite"
)

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

type model struct {
	table table.Model
	err   error
}

type appInfo struct {
	Name   string
	Domain string
	Status string
	Health string
}

func (m model) Init() tea.Cmd {
	return tick()
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Every(time.Second*2, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	case tickMsg:
		m.err = m.refresh()
		return m, tick()
	case error:
		m.err = msg
		return m, nil
	}
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *model) refresh() error {
	dbPath := getenv("GODEPLOY_DB", "godeploy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer docker.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Get routes from DB
	rows, err := db.QueryContext(ctx, "SELECT domain, target FROM proxy_routes")
	if err != nil {
		return err
	}
	defer rows.Close()

	domainToTarget := make(map[string]string)
	for rows.Next() {
		var d, t string
		if err := rows.Scan(&d, &t); err == nil {
			domainToTarget[d] = t
		}
	}

	// 2. Get managed containers from Docker
	args := filters.NewArgs()
	args.Add("label", "godeploy.managed=true")
	containers, err := docker.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
	if err != nil {
		return err
	}

	appMap := make(map[string]*appInfo)

	for _, c := range containers {
		name := c.Labels["godeploy.app.name"]
		if name == "" {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		status := c.Status
		health := "Unknown"
		if strings.Contains(strings.ToLower(c.State), "running") {
			health = "Healthy"
		} else {
			health = "Unhealthy"
		}

		// Find domain for this app
		domain := "-"
		for d, t := range domainToTarget {
			// This is a bit heuristic: if target port is in container ports
			for _, p := range c.Ports {
				targetPort := fmt.Sprintf(":%d", p.PublicPort)
				if strings.HasSuffix(t, targetPort) {
					domain = d
					break
				}
			}
			if domain != "-" {
				break
			}
		}

		if existing, ok := appMap[name]; ok {
			// If we have multiple containers for the same app name, prefer the running one
			if health == "Healthy" || existing.Health != "Healthy" {
				appMap[name] = &appInfo{Name: name, Domain: domain, Status: status, Health: health}
			}
		} else {
			appMap[name] = &appInfo{Name: name, Domain: domain, Status: status, Health: health}
		}
	}

	rows_table := []table.Row{}
	for _, app := range appMap {
		rows_table = append(rows_table, table.Row{app.Name, app.Domain, app.Status, app.Health})
	}

	m.table.SetRows(rows_table)
	return nil
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("\n Error: %v\n\n (press q to quit)", m.err)
	}
	return baseStyle.Render(m.table.View()) + "\n (q to quit, auto-refreshes every 2s)\n"
}

func main() {
	columns := []table.Column{
		{Title: "Application", Width: 20},
		{Title: "Domain", Width: 30},
		{Title: "Status", Width: 20},
		{Title: "Health", Width: 10},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	m := model{table: t}
	_ = m.refresh() // initial load

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
