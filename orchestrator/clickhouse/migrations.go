package clickhouse

import (
	"context"
	"fmt"
	"net"

	"github.com/ClickHouse/clickhouse-go/v2"

	"akvorado/common/reporter"
)

type migrationStep struct {
	// CheckQuery to execute to check if the step is needed.
	CheckQuery string
	// Arguments to use for the query
	Args []interface{}
	// Function to execute if the query returns no row or returns `0'.
	Do func() error
}

// migrateDatabase execute database migration
func (c *Component) migrateDatabase() error {
	if c.config.OrchestratorURL == "" {
		baseURL, err := c.getHTTPBaseURL("1.1.1.1:80")
		if err != nil {
			return err
		}
		c.config.OrchestratorURL = baseURL
	}

	ctx := c.t.Context(nil)
	steps := []struct {
		Description string
		Step        func(context.Context, reporter.Logger, clickhouse.Conn) migrationStep
	}{
		{"create flows table", c.migrateStepCreateFlowsTable},
		{"add ForwardingStatus to flows table", c.migrateStepAddForwardingStatusFlowsTable},
		{"create exporters view", c.migrateStepCreateExportersView},
		{"create protocols dictionary", c.migrateStepCreateProtocolsDictionary},
		{"create asns dictionary", c.migrateStepCreateASNsDictionary},
		{"create raw flows table", c.migrateStepCreateRawFlowsTable},
		{"create raw flows consumer view", c.migrateStepCreateRawFlowsConsumerView},
		{"add expiration to flow table", c.migrateStepAddExpirationFlowsTable},
		{"drop schema_migrations table", c.migrateStepDropSchemaMigrationsTable},
	}

	count := 0
	total := 0
	for _, step := range steps {
		total++
		l := c.r.Logger.With().Str("step", step.Description).Logger()
		l.Debug().Msg("checking migration step")
		step := step.Step(ctx, l, c.d.ClickHouse)
		rows, err := c.d.ClickHouse.Query(ctx, step.CheckQuery, step.Args...)
		if err != nil {
			l.Err(err).Msg("cannot execute check")
			return fmt.Errorf("cannot execute check: %w", err)
		}
		if rows.Next() {
			var val uint8
			if err := rows.Scan(&val); err != nil {
				rows.Close()
				l.Err(err).Msg("cannot parse check result")
				return fmt.Errorf("cannot parse check result: %w", err)
			}
			if val != 0 {
				rows.Close()
				l.Debug().Msg("result not equal to 0, skipping step")
				c.metrics.migrationsNotApplied.Inc()
				continue
			} else {
				l.Debug().Msg("got 0, executing step")
			}
		} else {
			l.Debug().Msg("no result, executing step")
		}
		rows.Close()
		if err := step.Do(); err != nil {
			l.Err(err).Msg("cannot execute migration step")
			return fmt.Errorf("during migration step: %w", err)
		}
		l.Info().Msg("migration step executed successfully")
		c.metrics.migrationsApplied.Inc()
		count++
	}

	if count == 0 {
		c.r.Debug().Msg("no migration needed")
	} else {
		c.r.Info().Msg("migrations done")
	}
	close(c.migrationsDone)
	c.metrics.migrationsRunning.Set(0)
	c.metrics.migrationsVersion.Set(float64(total))

	return nil
}

// getHTTPBaseURL tries to guess the appropriate URL to access our
// HTTP daemon. It tries to get our IP address using an unconnected
// UDP socket.
func (c *Component) getHTTPBaseURL(address string) (string, error) {
	// Get IP address
	conn, err := net.Dial("udp", address)
	if err != nil {
		return "", fmt.Errorf("cannot get our IP address: %w", err)
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)

	// Combine with HTTP port
	_, port, err := net.SplitHostPort(c.d.HTTP.Address.String())
	if err != nil {
		return "", fmt.Errorf("cannot get HTTP port: %w", err)
	}
	base := fmt.Sprintf("http://%s",
		net.JoinHostPort(localAddr.IP.String(), port))
	c.r.Debug().Msgf("detected base URL is %s", base)
	return base, nil
}
