package main

import (
	"bufio"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/fsnotify/fsnotify"
)

type appConfig struct {
	rootPath     string
	colorEnabled bool
	pollInterval time.Duration
	prefixMode   string // full | relative | base
	include      []string
	exclude      []string
	linesOpt     lineOption
}

type lineOption struct {
	enabled  bool
	n        int
	fromHead bool // true for +NUM semantics
}

func main() {
	pwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	var rootPath string

	rootPath, linesOpt, err := parseArgs(os.Args[1:], pwd)
	if err != nil {
		log.Fatal(err)
	}

	colorEnabled := true
	if v, ok := os.LookupEnv("TAILX_COLOR"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			colorEnabled = b
		}
	}

	cfg := appConfig{
		rootPath:     rootPath,
		colorEnabled: colorEnabled,
		prefixMode:   strings.ToLower(os.Getenv("TAILX_PREFIX")),
		pollInterval: 200 * time.Millisecond,
		include:      splitList(os.Getenv("TAILX_INCLUDE")),
		exclude:      splitList(os.Getenv("TAILX_EXCLUDE")),
		linesOpt:     linesOpt,
	}
	if cfg.prefixMode == "" {
		cfg.prefixMode = "full"
	}
	if v := os.Getenv("TAILX_POLL_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			cfg.pollInterval = time.Duration(ms) * time.Millisecond
		}
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	tailedFiles := make(map[string]struct{})
	watchedPaths := make(map[string]struct{})
	var mu sync.Mutex

	var tryTail func(string, bool)

	addWatch := func(path string) error {
		if _, ok := watchedPaths[path]; ok {
			return nil
		}
		if err := watcher.Add(path); err != nil {
			return err
		}
		watchedPaths[path] = struct{}{}
		return nil
	}

	tryTail = func(file string, fromStart bool) {
		if !shouldTail(file, cfg) {
			return
		}
		info, err := os.Stat(file)
		if err != nil || info.IsDir() {
			return
		}
		mu.Lock()
		if _, ok := tailedFiles[file]; ok {
			mu.Unlock()
			return
		}
		tailedFiles[file] = struct{}{}
		mu.Unlock()
		go tailFile(file, fromStart, cfg, func() {
			mu.Lock()
			delete(tailedFiles, file)
			mu.Unlock()
		})
	}

	walkAddDirs := func(path string) error {
		return filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return addWatch(p)
			}
			tryTail(p, false)
			return nil
		})
	}

	if err := walkAddDirs(rootPath); err != nil {
		log.Fatal(err)
	}

	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() { closeOnce.Do(func() { close(done) }) }

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					closeDone()
					return
				}
				// New directory: start watching it so nested files get picked up.
				if event.Has(fsnotify.Create) {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						if err := walkAddDirs(event.Name); err != nil {
							log.Println("error walking new dir:", err)
						}
						continue
					}
				}
				if event.Has(fsnotify.Create) {
					tryTail(event.Name, true)
				} else if event.Has(fsnotify.Write) || event.Has(fsnotify.Chmod) {
					tryTail(event.Name, false)
				}
				if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
					mu.Lock()
					delete(tailedFiles, event.Name)
					mu.Unlock()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					closeDone()
					return
				}
				log.Println("error:", err)
				closeDone()
			}
		}
	}()

	<-done
}

func tailFile(file string, fromStart bool, cfg appConfig, cleanup func()) {
	defer cleanup()

	f, err := os.Open(file)
	if err != nil {
		log.Println("open file error:", err)
		return
	}
	defer f.Close()

	startSet := false
	if cfg.linesOpt.enabled {
		if cfg.linesOpt.fromHead {
			if off, err := seekFromHead(f, cfg.linesOpt.n); err == nil {
				if _, err := f.Seek(off, io.SeekStart); err == nil {
					startSet = true
				}
			}
		} else {
			if off, err := seekLastNLines(f, cfg.linesOpt.n); err == nil {
				if _, err := f.Seek(off, io.SeekStart); err == nil {
					startSet = true
				}
			}
		}
	}

	if !startSet && !fromStart {
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			log.Println("seek error:", err)
			return
		}
	}

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			time.Sleep(cfg.pollInterval)
			continue
		}
		if err != nil {
			log.Println("tail error:", err)
			return
		}
		colorize(file, line, cfg)
	}
}

var palette = []*color.Color{}

func colorize(file, line string, cfg appConfig) {
	idx := crc32.ChecksumIEEE([]byte(file)) % uint32(len(palette))
	name := formatName(file, cfg)
	if !cfg.colorEnabled {
		fmt.Print("[", name, "] ", line)
		return
	}
	prefix := palette[idx].Sprint("[", name, "]")
	fmt.Print(prefix, " ", line)
}

func init() {
	attrs := []color.Attribute{
		color.FgCyan, color.FgGreen, color.FgYellow, color.FgMagenta, color.FgBlue, color.FgWhite,
		color.FgRed, color.FgBlack,
		color.FgHiCyan, color.FgHiGreen, color.FgHiYellow, color.FgHiMagenta, color.FgHiBlue, color.FgHiWhite,
		color.FgHiRed, color.FgHiBlack,
	}
	for _, a := range attrs {
		palette = append(palette, color.New(a))
	}
}

func splitList(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func shouldTail(file string, cfg appConfig) bool {
	for _, ex := range cfg.exclude {
		if strings.Contains(file, ex) {
			return false
		}
	}
	if len(cfg.include) == 0 {
		return true
	}
	for _, in := range cfg.include {
		if strings.Contains(file, in) {
			return true
		}
	}
	return false
}

func formatName(file string, cfg appConfig) string {
	switch cfg.prefixMode {
	case "base":
		return filepath.Base(file)
	case "relative":
		if rel, err := filepath.Rel(cfg.rootPath, file); err == nil {
			return rel
		}
	}
	return file
}

func parseArgs(args []string, pwd string) (string, lineOption, error) {
	var rootPath string
	var lines lineOption

	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "-n" || arg == "--lines" {
			if i+1 >= len(args) {
				return "", lineOption{}, fmt.Errorf("missing value for %s", arg)
			}
			val := args[i+1]
			i += 2
			opt, err := parseLinesValue(val)
			if err != nil {
				return "", lineOption{}, err
			}
			lines = opt
			continue
		}
		if strings.HasPrefix(arg, "--lines=") {
			val := strings.TrimPrefix(arg, "--lines=")
			opt, err := parseLinesValue(val)
			if err != nil {
				return "", lineOption{}, err
			}
			lines = opt
			i++
			continue
		}
		if strings.HasPrefix(arg, "-n") && len(arg) > 2 {
			val := strings.TrimPrefix(arg, "-n")
			opt, err := parseLinesValue(val)
			if err != nil {
				return "", lineOption{}, err
			}
			lines = opt
			i++
			continue
		}
		// first non-flag arg as root path
		if rootPath == "" {
			rootPath = arg
		}
		i++
	}

	if rootPath == "" {
		rootPath = pwd
	}
	return rootPath, lines, nil
}

func parseLinesValue(v string) (lineOption, error) {
	opt := lineOption{enabled: true}
	if strings.HasPrefix(v, "+") {
		opt.fromHead = true
		v = strings.TrimPrefix(v, "+")
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return lineOption{}, fmt.Errorf("invalid -n/--lines value: %s", v)
	}
	opt.n = n
	return opt, nil
}

func seekFromHead(f *os.File, n int) (int64, error) {
	if n <= 1 {
		return 0, nil
	}
	skip := n - 1
	reader := bufio.NewReader(f)
	var offset int64
	for skip > 0 {
		b, err := reader.ReadBytes('\n')
		offset += int64(len(b))
		if err != nil {
			if err == io.EOF {
				return offset, nil
			}
			return 0, err
		}
		skip--
	}
	return offset, nil
}

func seekLastNLines(f *os.File, n int) (int64, error) {
	if n <= 0 {
		return 0, nil
	}
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := info.Size()
	if size == 0 {
		return 0, nil
	}
	var (
		bufSize int64 = 4096
		pos           = size
		needed        = n
	)
	for pos > 0 {
		step := bufSize
		if pos < step {
			step = pos
		}
		pos -= step
		buf := make([]byte, int(step))
		nRead, err := f.ReadAt(buf, pos)
		if err != nil && err != io.EOF {
			if err != io.ErrUnexpectedEOF {
				return 0, err
			}
		}
		buf = buf[:nRead]
		for i := len(buf) - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				needed--
				if needed == 0 {
					return pos + int64(i+1), nil
				}
			}
		}
	}
	return 0, nil
}
