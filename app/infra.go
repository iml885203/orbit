package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/container"
	"github.com/iml885203/orbit/platform"
	"github.com/spf13/cobra"
)

func execCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exec <container> [command...]",
		Short: "Run a command inside a container",
		Long: `Run a command inside a running container.

Examples:
  orbit exec redis redis-cli PING
  orbit exec mongodb mongosh
  orbit exec database /opt/mssql-tools18/bin/sqlcmd -S localhost -U sa`,
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: true,
		RunE:               runExec,
	}
}

func queryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query <type> [args...]",
		Short: "Query infrastructure databases",
		Long: `Query infrastructure databases using the container's built-in client.

Examples:
  orbit query mongo mydb '{"name":"test"}'
  orbit query postgres "SELECT current_database();"
  orbit query redis GET session:123`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "mongo [db] [query]",
		Short: "Query MongoDB",
		Long: `Query MongoDB using mongosh.

Examples:
  orbit query mongo                            # interactive shell
  orbit query mongo mydb                       # connect to specific db
  orbit query mongo mydb '{"name":"test"}'     # run a query`,
		RunE: runQueryMongo,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "redis [command...]",
		Short: "Query Redis",
		Long: `Query Redis using redis-cli.

Examples:
  orbit query redis                           # interactive mode
  orbit query redis GET session:123
  orbit query redis KEYS '*'`,
		RunE: runQueryRedis,
	})
	postgres := &cobra.Command{
		Use:     "postgres [sql]",
		Aliases: []string{"postgresql"},
		Short:   "Query PostgreSQL",
		Long: `Query PostgreSQL using psql inside the configured container.

The container's POSTGRES_USER and POSTGRES_DB select the default connection.

Examples:
  orbit query postgres
  orbit query postgres "SELECT current_database();"
  orbit query postgres --database app "SELECT * FROM users LIMIT 5"`,
		Args: cobra.ArbitraryArgs,
	}
	var postgresDatabase string
	postgres.Flags().StringVarP(&postgresDatabase, "database", "d", "", "database name (defaults to POSTGRES_DB)")
	postgres.RunE = func(_ *cobra.Command, args []string) error {
		return runQueryPostgres(postgresDatabase, args)
	}
	cmd.AddCommand(postgres)
	return cmd
}

func runExec(cmd *cobra.Command, args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return cmd.Help()
	}

	name := args[0]
	containerName := orbitContainerName(name)

	cmdArgs := []string{"exec", "-it", containerName}
	if len(args) > 1 {
		cmdArgs = append(cmdArgs, args[1:]...)
	} else {
		cmdArgs = append(cmdArgs, "sh")
	}

	return execDocker(cmdArgs)
}

func runQueryMongo(_ *cobra.Command, args []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	container := findContainer(cfg, "mongo")
	if container == "" {
		return fmt.Errorf("no MongoDB container found in config")
	}

	cmdArgs := []string{"exec", "-it", orbitContainerName(container), "mongosh", "--quiet"}

	if len(args) >= 1 {
		cmdArgs = append(cmdArgs, args[0]) // db name
	}
	if len(args) >= 2 {
		cmdArgs = append(cmdArgs, "--eval", strings.Join(args[1:], " "))
	}

	return execDocker(cmdArgs)
}

func runQueryRedis(_ *cobra.Command, args []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	container := findContainer(cfg, "redis")
	if container == "" {
		return fmt.Errorf("no Redis container found in config")
	}

	cmdArgs := make([]string, 0, 4+len(args))
	cmdArgs = append(cmdArgs, "exec", "-it", orbitContainerName(container), "redis-cli")
	cmdArgs = append(cmdArgs, args...)

	return execDocker(cmdArgs)
}

func runQueryPostgres(database string, queryParts []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	container := findPostgresContainer(cfg)
	if container == "" {
		return fmt.Errorf("no PostgreSQL container found in config")
	}

	return execDocker(postgresQueryDockerArgs(orbitContainerName(container), database, queryParts))
}

func postgresQueryDockerArgs(containerName, database string, queryParts []string) []string {
	const runPSQL = `user="${POSTGRES_USER:-postgres}"
database="${1:-${POSTGRES_DB:-$user}}"
if [ -n "${POSTGRES_PASSWORD:-}" ] && [ -z "${PGPASSWORD:-}" ]; then
  export PGPASSWORD="$POSTGRES_PASSWORD"
fi
if [ -n "$2" ]; then
  exec psql -U "$user" -d "$database" -c "$2"
fi
exec psql -U "$user" -d "$database"`

	query := ""
	if len(queryParts) > 0 {
		query = strings.Join(queryParts, " ")
	}
	return []string{
		"exec", "-it", containerName,
		"/bin/sh", "-c", runPSQL,
		"orbit-query-postgres",
		database,
		query,
	}
}

func topicsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topics",
		Short: "Manage Kafka topics",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all Kafka topics",
		RunE:  runTopicsList,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "create <topic>",
		Short: "Create a Kafka topic",
		Args:  cobra.ExactArgs(1),
		RunE:  runTopicsCreate,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "describe <topic>",
		Short: "Describe a Kafka topic",
		Args:  cobra.ExactArgs(1),
		RunE:  runTopicsDescribe,
	})
	return cmd
}

func runTopicsList(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	container, err := findKafkaContainer(cfg)
	if err != nil {
		return err
	}

	cmd := exec.Command("docker", "exec", orbitContainerName(container),
		"kafka-topics", "--bootstrap-server", kafkaBootstrapAddr, "--list")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runTopicsCreate(_ *cobra.Command, args []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	container, err := findKafkaContainer(cfg)
	if err != nil {
		return err
	}

	cmd := exec.Command("docker", "exec", orbitContainerName(container),
		"kafka-topics", "--bootstrap-server", kafkaBootstrapAddr,
		"--create", "--topic", args[0], "--partitions", "1", "--replication-factor", "1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runTopicsDescribe(_ *cobra.Command, args []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	container, err := findKafkaContainer(cfg)
	if err != nil {
		return err
	}

	cmd := exec.Command("docker", "exec", orbitContainerName(container),
		"kafka-topics", "--bootstrap-server", kafkaBootstrapAddr,
		"--describe", "--topic", args[0])
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var seedForce bool

func seedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seed [containers...]",
		Short: "Run seed data scripts",
		Long: `Run seed data scripts for infrastructure containers.

Examples:
  orbit seed                    # seed all containers with seed config
  orbit seed sql-server         # seed only SQL Server
  orbit seed --force            # re-run all seeds (even previously executed)`,
		RunE: runSeed,
	}
	cmd.Flags().BoolVar(&seedForce, "force", false, "re-run all seeds regardless of previous execution")
	return cmd
}

func runSeed(_ *cobra.Command, args []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	filter := make(map[string]bool, len(args))
	for _, a := range args {
		filter[a] = true
	}

	anySeeds := false
	var okCount, failCount, skipCount int
	for name, c := range cfg.Containers {
		if len(filter) > 0 && !filter[name] {
			continue
		}
		if c.Seed == nil || len(c.Seed.Files) == 0 {
			continue
		}
		anySeeds = true

		_, _ = cli.Bold.Printf("%s\n", name)
		results := container.RunSeed(name, c, seedForce)
		for _, r := range results {
			switch r.Status {
			case "executed":
				fmt.Printf("  %s %s\n", cli.Green.Sprint("✓"), r.File)
				okCount++
			case "skipped":
				fmt.Printf("  %s %s %s\n", cli.Faint.Sprint("–"), r.File, cli.Faint.Sprint("(already run)"))
				skipCount++
			case "changed":
				fmt.Printf("  %s %s %s\n", cli.Yellow.Sprint("~"), r.File, cli.Yellow.Sprint("(changed, use --force)"))
				skipCount++
			case "failed":
				fmt.Printf("  %s %s %s\n", cli.Red.Sprint("✗"), r.File, cli.Red.Sprint(r.Message))
				failCount++
			}
		}
	}

	if !anySeeds {
		fmt.Println("No seed configuration found. Add 'seed:' to container config.")
		return nil
	}

	fmt.Printf("\n%s succeeded, %s failed, %s skipped\n",
		cli.Green.Sprintf("%d ✓", okCount),
		cli.Red.Sprintf("%d ✗", failCount),
		cli.Faint.Sprintf("%d –", skipCount))

	if failCount > 0 {
		return fmt.Errorf("%d seed file(s) failed", failCount)
	}
	return nil
}

// kafkaBootstrapAddr is re-exported from the container package for use in CLI commands.
var kafkaBootstrapAddr = container.KafkaInternalBootstrap

func findKafkaContainer(cfg *config.Config) (string, error) {
	container := findContainer(cfg, "broker")
	if container == "" {
		container = findContainer(cfg, "kafka")
	}
	if container == "" {
		return "", fmt.Errorf("no Kafka container found in config")
	}
	return container, nil
}

// findContainer finds a container by a declared port label.
func findContainer(cfg *config.Config, portLabel string) string {
	for name, c := range cfg.Containers {
		for label := range c.Ports {
			if label == portLabel {
				return name
			}
		}
	}
	// Fallback: match by container name
	for name := range cfg.Containers {
		if strings.Contains(name, portLabel) {
			return name
		}
	}
	return ""
}

func findPostgresContainer(cfg *config.Config) string {
	if name := findContainer(cfg, "postgres"); name != "" {
		return name
	}
	return findContainer(cfg, "postgresql")
}

// execDocker replaces the current process with docker exec (no subprocess overhead).
// On Unix, uses syscall.Exec for proper TTY passthrough. On Windows, falls back to cmd.Run.
func execDocker(args []string) error {
	// Replace -it with -i (or -it if TTY available)
	for i, a := range args {
		if a == "-it" {
			if isTerminal() {
				args[i] = "-it"
			} else {
				args[i] = "-i"
			}
		}
	}

	return platform.ExecReplace("docker", args)
}

// orbitContainerName resolves a service name to its Docker container name,
// honoring the ORBIT_NAMESPACE env var for isolation.
func orbitContainerName(svc string) string {
	return container.ContainerName(os.Getenv("ORBIT_NAMESPACE"), svc)
}
