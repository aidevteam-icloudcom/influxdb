package run_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCluster_CreateDatabase(t *testing.T) {
	t.Parallel()

	c, err := NewClusterWithDefaults(5)
	defer c.Close()
	if err != nil {
		t.Fatalf("error creating cluster: %s", err)
	}
}

func TestCluster_Write(t *testing.T) {
	t.Parallel()

	c, err := NewClusterWithDefaults(5)
	if err != nil {
		t.Fatalf("error creating cluster: %s", err)
	}
	defer c.Close()

	writes := []string{
		fmt.Sprintf(`cpu,host=serverA,region=uswest val=23.2 %d`, mustParseTime(time.RFC3339Nano, "2000-01-01T00:00:00Z").UnixNano()),
	}

	_, err = c.Servers[0].Write("db0", "default", strings.Join(writes, "\n"), nil)
	if err != nil {
		t.Fatal(err)
	}

	q := &Query{
		name:    "write",
		command: `SELECT * FROM db0."default".cpu`,
		exp:     `{"results":[{"series":[{"name":"cpu","columns":["time","host","region","val"],"values":[["2000-01-01T00:00:00Z","serverA","uswest",23.2]]}]}]}`,
	}
	err = c.QueryAll(q)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCluster_DatabaseCommands(t *testing.T) {
	t.Parallel()
	c, err := NewCluster(5)
	if err != nil {
		t.Fatalf("error creating cluster: %s", err)
	}

	defer c.Close()

	test := tests.load(t, "database_commands")

	for _, query := range test.queries {
		if query.skip {
			t.Logf("SKIP:: %s", query.name)
			continue
		}
		t.Logf("Running %s", query.name)
		if query.once {
			if _, err := c.Query(query); err != nil {
				t.Error(query.Error(err))
			} else if !query.success() {
				t.Error(query.failureMessage())
			}
			continue
		}
		if err := c.QueryAll(query); err != nil {
			t.Error(query.Error(err))
		}
	}
}

func TestCluster_Query_DropAndRecreateDatabase(t *testing.T) {
	t.Parallel()
	c, err := NewClusterWithDefaults(5)
	if err != nil {
		t.Fatalf("error creating cluster: %s", err)
	}
	defer c.Close()

	test := tests.load(t, "drop_and_recreate_database")

	_, err = c.Servers[0].Write(test.database(), test.retentionPolicy(), test.write, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, query := range test.queries {
		if query.skip {
			t.Logf("SKIP:: %s", query.name)
			continue
		}
		t.Logf("Running %s", query.name)
		if query.once {
			if _, err := c.Query(query); err != nil {
				t.Error(query.Error(err))
			} else if !query.success() {
				t.Error(query.failureMessage())
			}
			continue
		}
		if err := c.QueryAll(query); err != nil {
			t.Error(query.Error(err))
		}
	}
}

func TestCluster_Query_DropDatabaseIsolated(t *testing.T) {
	t.Parallel()
	c, err := NewCluster(5)
	if err != nil {
		t.Fatalf("error creating cluster: %s", err)
	}
	defer c.Close()

	test := tests.load(t, "drop_database_isolated")

	s := c.Servers[0]
	if err := s.CreateDatabaseAndRetentionPolicy("db0", newRetentionPolicyInfo("rp0", 1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := s.MetaStore.SetDefaultRetentionPolicy("db0", "rp0"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateDatabaseAndRetentionPolicy("db1", newRetentionPolicyInfo("rp1", 1, 0)); err != nil {
		t.Fatal(err)
	}

	_, err = s.Write(test.database(), test.retentionPolicy(), test.write, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, query := range test.queries {
		if query.skip {
			t.Logf("SKIP:: %s", query.name)
			continue
		}
		t.Logf("Running %s", query.name)
		if query.once {
			if _, err := c.Query(query); err != nil {
				t.Error(query.Error(err))
			} else if !query.success() {
				t.Error(query.failureMessage())
			}
			continue
		}
		if err := c.QueryAll(query); err != nil {
			t.Error(query.Error(err))
		}
	}
}
