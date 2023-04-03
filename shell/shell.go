package shell

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/summerwind/gptask/log"
)

const magicDelimiter = "GPTASK-COMMAND-END"

type Shell struct {
	workDir   string
	cmd       *exec.Cmd
	stdinPipe io.WriteCloser
	stdoutCh  chan string
	stderrCh  chan string
}

func New() *Shell {
	return &Shell{
		stdoutCh: make(chan string),
		stderrCh: make(chan string),
	}
}

func (s *Shell) Start() error {
	s.cmd = exec.Command("bash", "-o", "pipefail", "-s")

	stdin, err := s.cmd.StdinPipe()
	if err != nil {
		return err
	}
	s.stdinPipe = stdin

	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := s.cmd.StderrPipe()
	if err != nil {
		return err
	}

	go func(ch chan<- string) {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			ch <- line
		}
	}(s.stdoutCh)

	go func(ch chan<- string) {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			ch <- line
		}
	}(s.stderrCh)

	err = s.cmd.Start()
	if err != nil {
		return err
	}
	log.Debug("shell", "start")

	return nil
}

func (s *Shell) Run(cmd string) (int, string, string, error) {
	var (
		stdout []string
		stderr []string
		err    error
	)

	_, err = io.WriteString(s.stdinPipe, fmt.Sprintf("%s\necho \"%s,$?,$PWD\"\n", cmd, magicDelimiter))
	if err != nil {
		return -1, "", "", err
	}

	for {
		select {
		case line := <-s.stdoutCh:
			magicIndex := strings.Index(line, magicDelimiter)
			if magicIndex != -1 {
				if magicIndex != 0 {
					l := line[:magicIndex]
					log.Stdout(l)
					stdout = append(stdout, l)
				}

				info := strings.Split(line[magicIndex:], ",")
				rc, _ := strconv.Atoi(info[1])
				s.workDir = info[2]

				log.Debug("exit-code", fmt.Sprintf("%d", rc))
				return rc, strings.Join(stdout, "\n"), strings.Join(stderr, "\n"), nil
			}

			log.Stdout(line)
			stdout = append(stdout, line)
		case line := <-s.stderrCh:
			log.Stderr(line)
			stderr = append(stderr, line)
		}
	}
}

func (s *Shell) Stop() error {
	if s.stdinPipe == nil {
		return nil
	}

	s.stdinPipe.Close()
	return s.cmd.Wait()
}

func (s *Shell) WorkDir() string {
	return s.workDir
}
