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

	"godeploy-platform/internal/platform/iox"
	"godeploy-platform/internal/platform/sqlpool"

	_ "modernc.org/sqlite"
)

const (
	healthLabelHealthy   = "Healthy"
	healthLabelUnhealthy = "Unhealthy"
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
	sqlpool.ForSQLite(db)
	defer iox.Close(db)

	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer iox.Close(docker)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	domainToTarget, err := loadProxyRoutes(ctx, db)
	if err != nil {
		return err
	}

	containers, err := listManagedContainers(ctx, docker)
	if err != nil {
		return err
	}

	appMap := buildAppMapFromContainers(containers, domainToTarget)
	rows := make([]table.Row, 0, len(appMap))
	for _, app := range appMap {
		rows = append(rows, table.Row{app.Name, app.Domain, app.Status, app.Health})
	}
	m.table.SetRows(rows)
	return nil
}

func loadProxyRoutes(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT domain, target FROM proxy_routes") //nolint:sqlclosecheck // rows closed in defer below
	if err != nil {
		return nil, err
	}
	defer iox.Close(rows)

	out := make(map[string]string)
	for rows.Next() {
		var d, t string
		if scanErr := rows.Scan(&d, &t); scanErr == nil {
			out[d] = t
		}
	}
	return out, nil
}

func listManagedContainers(ctx context.Context, docker *client.Client) ([]container.Summary, error) {
	args := filters.NewArgs()
	args.Add("label", "godeploy.managed=true")
	return docker.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
}

func buildAppMapFromContainers(containers []container.Summary, domainToTarget map[string]string) map[string]*appInfo {
	appMap := make(map[string]*appInfo)
	for _, c := range containers {
		info := containerToAppInfo(c, domainToTarget)
		mergeAppInfo(appMap, info)
	}
	return appMap
}

func containerToAppInfo(c container.Summary, domainToTarget map[string]string) *appInfo {
	name := c.Labels["godeploy.app.name"]
	if name == "" {
		name = strings.TrimPrefix(c.Names[0], "/")
	}
	status := c.Status
	health := healthLabelUnhealthy
	if strings.Contains(strings.ToLower(c.State), "running") {
		health = healthLabelHealthy
	}
	domain := domainForContainerPorts(c, domainToTarget)
	return &appInfo{Name: name, Domain: domain, Status: status, Health: health}
}

func domainForContainerPorts(c container.Summary, domainToTarget map[string]string) string {
	for d, t := range domainToTarget {
		for _, p := range c.Ports {
			targetPort := fmt.Sprintf(":%d", p.PublicPort)
			if strings.HasSuffix(t, targetPort) {
				return d
			}
		}
	}
	return "-"
}

func mergeAppInfo(appMap map[string]*appInfo, info *appInfo) {
	existing, ok := appMap[info.Name]
	if !ok {
		appMap[info.Name] = info
		return
	}
	if info.Health == healthLabelHealthy || existing.Health != healthLabelHealthy {
		appMap[info.Name] = info
	}
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
	if err := m.refresh(); err != nil {
		log.Printf("refresh inicial: %v", err)
	}

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
