// Command dzzzr is a command-line client for the Dozor Classic city-game
// engine (classic.dzzzr.ru). It plays a game as a team member and, with
// organizer credentials, administers one.
//
// User-facing text is Russian because the engine and its players are.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// version is overridden at build time with -ldflags "-X main.version=…".
var version = "dev"

// authLevel says which credentials a command needs before it runs.
type authLevel int

const (
	// authNone commands talk to endpoints that need no credentials.
	authNone authLevel = iota
	// authOptional commands read pages the engine serves to anyone but shows
	// more of to a signed-in visitor. A stored session is used when there is
	// one and the command runs either way.
	authOptional
	// authPlayer commands need a site session plus the captain login and PIN.
	authPlayer
	// authAdmin commands need the organizer login and password.
	authAdmin
)

// command is one entry of the dispatch table. Usage is the full argument
// line shown in help, Help a single Russian sentence.
type command struct {
	Name  string
	Usage string
	Help  string
	Auth  authLevel
	Run   func(ctx context.Context, cfg *config, c *dzzzr.Client, args []string) error
}

// commands is filled by the init functions of the files that define them.
var commands []command

// register adds commands to the dispatch table.
func register(cmds ...command) { commands = append(commands, cmds...) }

// findCommand looks a command up by name, returning nil when unknown.
func findCommand(name string) *command {
	for i := range commands {
		if commands[i].Name == name {
			return &commands[i]
		}
	}
	return nil
}

// config holds every global flag plus the streams the run is attached to,
// so tests can drive the whole program without touching os.Stdout.
type config struct {
	city          string
	login         string
	password      string
	captain       string
	pin           string
	adminLogin    string
	adminPassword string
	baseURL       string
	useHTTP       bool
	insecure      bool
	jsonOut       bool
	debug         bool
	har           bool
	harOut        string
	timeout       int
	showVersion   bool

	// Command-scoped flags. They live on the one global FlagSet because the
	// two-pass argument parser must know every flag before it can tell a
	// command name from a flag value.
	newOnly    bool
	archive    bool
	after      string
	withLevels bool
	since      string
	at         string
	team       int
	showAfter  string
	important  bool
	security   string
	withTasks  bool
	webAddr    string

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	// lineReader buffers stdin across the several prompts of "dzzzr login";
	// a fresh bufio.Reader per prompt would drop what it had read ahead.
	stdinReader *bufio.Reader
}

// lineReader returns the buffered reader over stdin.
func (cfg *config) lineReader() *bufio.Reader {
	if cfg.stdinReader == nil {
		cfg.stdinReader = bufio.NewReader(cfg.stdin)
	}
	return cfg.stdinReader
}

// registerFlags declares the global flags. Defaults come from the
// environment so DZZZR_* works everywhere a flag does.
func (cfg *config) registerFlags(fs *flag.FlagSet) {
	fs.StringVar(&cfg.city, "city", envOr("DZZZR_CITY", "moscow"), "город (сегмент пути движка), env DZZZR_CITY")
	fs.StringVar(&cfg.login, "login", os.Getenv("DZZZR_LOGIN"), "логин сайта для входа, env DZZZR_LOGIN")
	fs.StringVar(&cfg.password, "password", os.Getenv("DZZZR_PASSWORD"), "пароль сайта, env DZZZR_PASSWORD")
	fs.StringVar(&cfg.captain, "captain", os.Getenv("DZZZR_CAPTAIN"), "логин капитана для игровой авторизации, env DZZZR_CAPTAIN")
	fs.StringVar(&cfg.pin, "pin", os.Getenv("DZZZR_PIN"), "игровой PIN команды, env DZZZR_PIN")
	fs.StringVar(&cfg.adminLogin, "admin-login", os.Getenv("DZZZR_ADMIN_LOGIN"), "логин организатора, env DZZZR_ADMIN_LOGIN")
	fs.StringVar(&cfg.adminPassword, "admin-password", os.Getenv("DZZZR_ADMIN_PASSWORD"), "пароль организатора, env DZZZR_ADMIN_PASSWORD")
	fs.StringVar(&cfg.baseURL, "base-url", os.Getenv("DZZZR_BASE_URL"), "адрес движка вместо стандартного, env DZZZR_BASE_URL")
	fs.BoolVar(&cfg.useHTTP, "http", false, "обращаться к стандартному хосту по http, а не https")
	fs.BoolVar(&cfg.insecure, "insecure", envBool("DZZZR_INSECURE"), "не проверять TLS-сертификат, env DZZZR_INSECURE")
	fs.BoolVar(&cfg.jsonOut, "json", false, "выводить данные в JSON")
	fs.BoolVar(&cfg.debug, "debug", envBool("DZZZR_DEBUG"), "печатать HTTP-запросы в stderr, env DZZZR_DEBUG")
	fs.BoolVar(&cfg.har, "har", envBool("DZZZR_HAR"), "записывать HTTP-трафик в HAR, env DZZZR_HAR")
	fs.StringVar(&cfg.harOut, "har-out", os.Getenv("DZZZR_HAR_OUT"), "файл для HAR-записи (включает -har), env DZZZR_HAR_OUT")
	fs.IntVar(&cfg.timeout, "timeout", 30, "таймаут HTTP-запроса в секундах")
	fs.BoolVar(&cfg.showVersion, "version", false, "показать версию и выйти")
	fs.BoolVar(&cfg.showVersion, "v", false, "то же, что -version")

	fs.BoolVar(&cfg.newOnly, "new", false, "только предстоящие игры (games)")
	fs.BoolVar(&cfg.archive, "archive", false, "архив завершённых игр (games)")
	fs.StringVar(&cfg.after, "after", "", "показывать записи после «ГГГГ-ММ-ДД ЧЧ:ММ:СС» (messages)")
	fs.BoolVar(&cfg.withLevels, "with-levels", false, "копировать игру вместе с заданиями (admin-copy-game)")
	fs.StringVar(&cfg.since, "since", "", "события после «ГГГГ-ММ-ДД ЧЧ:ММ:СС» (admin-log)")
	fs.StringVar(&cfg.at, "at", "", "время события «ГГГГ-ММ-ДД ЧЧ:ММ:СС» вместо текущего (admin-give-level, admin-accept-level, admin-accept-code)")
	fs.IntVar(&cfg.team, "team", 0, "номер команды, 0 — все команды (admin-messages, admin-send-message)")
	fs.StringVar(&cfg.showAfter, "show-after", "", "показать сообщение командам после «ГГГГ-ММ-ДД ЧЧ:ММ:СС» (admin-send-message)")
	fs.BoolVar(&cfg.important, "important", false, "пометить сообщение как важное (admin-send-message)")
	fs.StringVar(&cfg.security, "security", "readonly", "права агента: readonly, approve или full (mcp)")
	fs.BoolVar(&cfg.withTasks, "levels", false, "показывать колонки заданий, а не только итоги (game-stat)")
	fs.StringVar(&cfg.webAddr, "web-addr", envOr("DZZZR_WEB_ADDR", defaultWebAddr), "адрес браузерного чата, env DZZZR_WEB_ADDR (web)")
}

// debugf writes a timestamped diagnostic line to stderr when -debug is on.
// It is also installed as the library's request logger.
func (cfg *config) debugf(format string, args ...any) {
	if !cfg.debug {
		return
	}
	fmt.Fprintf(cfg.stderr, "[%s] %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}

// envOr returns the environment variable or a fallback.
func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// envBool reads a boolean environment variable; anything but a recognised
// truthy word is false.
func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on", "y":
		return true
	}
	return false
}

// splitArgs parses flags and collects positional arguments in one pass over
// a single FlagSet, so flags may appear before the command, after it, or
// between its arguments without the parser knowing any flag names in
// advance. A "--" stops flag parsing; everything after it is positional.
//
// Positional arguments found before an error are returned as well, which is
// what lets "dzzzr send-code -h" print the help of send-code.
func splitArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	rest := args
	for len(rest) > 0 {
		if rest[0] == "--" {
			positional = append(positional, rest[1:]...)
			return positional, nil
		}
		if err := fs.Parse(rest); err != nil {
			return positional, err
		}
		p := fs.Args()
		if len(p) == 0 {
			return positional, nil
		}
		positional = append(positional, p[0])
		rest = p[1:]
	}
	return positional, nil
}

// printUsage writes the program's help: the two accepted argument orders,
// the command table split into player and organizer groups, and the flags.
func printUsage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprintf(w, "dzzzr %s — клиент движка «Дозор Классик»\n\n", version)
	fmt.Fprintln(w, "Использование:")
	fmt.Fprintln(w, "  dzzzr [флаги] <команда> [аргументы]")
	fmt.Fprintln(w, "  dzzzr <команда> [флаги] [аргументы]")

	names := make([]string, 0, len(commands))
	for _, c := range commands {
		names = append(names, c.Name)
	}
	slices.Sort(names)

	printGroup(w, "Команды игрока", names, func(n string) bool { return !isAdminCommand(n) })
	printGroup(w, "Команды организатора", names, isAdminCommand)

	fmt.Fprintln(w, "\nФлаги:")
	fs.SetOutput(w)
	fs.PrintDefaults()
	// The file mode is a promise only where the filesystem keeps it: on
	// Windows the session inherits the profile's access rules instead.
	name := "<город>.json"
	if dir, err := sessionDir(); err == nil {
		name = filepath.Join(dir, name)
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintf(w, "\nСессия хранится в %s.\n", name)
	} else {
		fmt.Fprintf(w, "\nСессия хранится в %s с правами 0600.\n", name)
	}
}

// printGroup writes the commands whose names satisfy keep, skipping the
// heading when the group is empty.
func printGroup(w io.Writer, title string, names []string, keep func(string) bool) {
	var selected []string
	for _, n := range names {
		if keep(n) {
			selected = append(selected, n)
		}
	}
	if len(selected) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s:\n", title)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, n := range selected {
		c := findCommand(n)
		fmt.Fprintf(tw, "  %s\t%s\n", c.Usage, c.Help)
	}
	_ = tw.Flush()
}

// isAdminCommand reports whether a command belongs to the organizer group.
func isAdminCommand(name string) bool { return strings.HasPrefix(name, "admin-") }

// printCommandHelp writes the help of a single command, as shown by
// "dzzzr <command> -h".
func printCommandHelp(w io.Writer, c *command, fs *flag.FlagSet) {
	fmt.Fprintf(w, "Использование: dzzzr [флаги] %s\n\n%s\n", c.Usage, c.Help)
	switch c.Auth {
	case authOptional:
		fmt.Fprintln(w, "\nРаботает без входа; сохранённая сессия используется, если она есть.")
	case authPlayer:
		fmt.Fprintln(w, "\nТребуется сессия сайта и игровые логин капитана с PIN.")
	case authAdmin:
		fmt.Fprintln(w, "\nТребуются логин и пароль организатора.")
	case authNone:
	}
	fmt.Fprintln(w, "\nФлаги:")
	fs.SetOutput(w)
	fs.PrintDefaults()
}

// newClient builds the engine client from the global flags.
func newClient(cfg *config) *dzzzr.Client {
	opts := []dzzzr.Option{
		dzzzr.WithUserAgent("dzzzr-cli/" + version),
		dzzzr.WithTimeout(time.Duration(max(cfg.timeout, 1)) * time.Second),
		dzzzr.WithDebugLogger(cfg.debugf),
		dzzzr.WithHARRecording(harEnabled(cfg)),
	}
	if cfg.useHTTP {
		opts = append(opts, dzzzr.WithHTTP())
	}
	if cfg.baseURL != "" {
		opts = append(opts, dzzzr.WithBaseURL(cfg.baseURL))
	}
	if cfg.insecure {
		opts = append(opts, dzzzr.WithInsecureTLS())
	}
	return dzzzr.New(cfg.city, opts...)
}

// harEnabled reports whether traffic must be captured.
func harEnabled(cfg *config) bool { return cfg.har || cfg.harOut != "" }

// writeHAR saves captured traffic once the command is done.
func writeHAR(cfg *config, c *dzzzr.Client) error {
	if !harEnabled(cfg) {
		return nil
	}
	path := cfg.harOut
	if path == "" {
		path = "dzzzr.har"
	}
	data, err := c.ExportHAR()
	if err != nil {
		return fatal("не удалось сформировать HAR: %v", err)
	}
	// A capture carries the game and, before redaction misses something, may
	// carry credentials, so it is written the way the session file is.
	if err := writeSecretFile(path, data); err != nil {
		return err
	}
	cfg.debugf("HAR записан в %s", path)
	return nil
}

// run executes one invocation and returns the process exit code: 0 on
// success, 2 for a usage problem or an action the engine rejected, 1 for
// everything else. It never calls os.Exit, so tests can call it directly.
func run(args []string, stdout, stderr io.Writer) int {
	cfg := &config{stdin: os.Stdin, stdout: stdout, stderr: stderr}
	fs := flag.NewFlagSet("dzzzr", flag.ContinueOnError)
	// The FlagSet stays quiet: its own message would be English and would
	// arrive before we know which command the user asked about. Both the
	// error and the help below are printed by hand.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	cfg.registerFlags(fs)

	positional, err := splitArgs(fs, args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		if len(positional) > 0 {
			if c := findCommand(positional[0]); c != nil {
				printCommandHelp(stdout, c, fs)
				return 0
			}
		}
		printUsage(stdout, fs)
		return 0
	case err != nil:
		fmt.Fprintf(stderr, "Ошибка: %v\n", err)
		fmt.Fprintln(stderr, "Подсказка: «dzzzr -h» показывает список команд.")
		return 2
	}

	if cfg.showVersion {
		fmt.Fprintf(stdout, "dzzzr %s\n", version)
		return 0
	}
	if len(positional) == 0 {
		printUsage(stderr, fs)
		return 2
	}

	name := positional[0]
	cmd := findCommand(name)
	if cmd == nil {
		fmt.Fprintf(stderr, "Ошибка: неизвестная команда %q\n", name)
		fmt.Fprintln(stderr, "Подсказка: «dzzzr -h» показывает список команд.")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c := newClient(cfg)
	cmdErr := authorize(ctx, cfg, c, cmd.Auth)
	if cmdErr == nil {
		cmdErr = cmd.Run(ctx, cfg, c, positional[1:])
		// A stored session outlives the game it was made for. When the engine
		// says it is stale and this run carries the site credentials, sign in
		// again rather than sending the player to «dzzzr login».
		if cmd.Auth == authPlayer && refreshSession(ctx, cfg, c, cmdErr) {
			cmdErr = cmd.Run(ctx, cfg, c, positional[1:])
		}
	}
	if harErr := writeHAR(cfg, c); harErr != nil && cmdErr == nil {
		cmdErr = harErr
	}
	return reportError(cfg, cmdErr)
}

// authorize prepares the credentials a command needs.
func authorize(ctx context.Context, cfg *config, c *dzzzr.Client, level authLevel) error {
	switch level {
	case authOptional:
		return optionalAuth(cfg, c)
	case authPlayer:
		return requireAuth(ctx, cfg, c)
	case authAdmin:
		return requireAdminAuth(cfg, c)
	case authNone:
	}
	return nil
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
