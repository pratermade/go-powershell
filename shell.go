// Copyright (c) 2017 Gorillalabs. All rights reserved.

package powershell

import (
	"fmt"
	"io"
	"sync"

	"github.com/pratermade/go-powershell.git/v2/backend"
	"github.com/pratermade/go-powershell.git/v2/utils"
)

const newline = "\r\n"

type Shell interface {
	Execute(cmd string) (string, string, error)
	Exit()
}

type shell struct {
	handle backend.Waiter
	stdin  io.Writer
	stdout io.Reader
	stderr io.Reader
}

func New(backend backend.Starter) (*shell, error) {
	handle, stdin, stdout, stderr, err := backend.StartProcess("powershell.exe", "-NoExit", "-Command", "-")
	if err != nil {
		return nil, err
	}

	return &shell{handle, stdin, stdout, stderr}, nil
}

func (s *shell) Execute(cmd string, stdout chan<- string, stderr chan<- string) {
	if s.handle == nil {
		stderr <- fmt.Sprintf("cannot execute on closed shells: %s", cmd)
		close(stderr)
		close(stdout)
	}

	outBoundary := createBoundary()
	errBoundary := createBoundary()

	// wrap the command in special markers so we know when to stop reading from the pipes
	full := fmt.Sprintf("%s; echo '%s'; [Console]::Error.WriteLine('%s')%s", cmd, outBoundary, errBoundary, newline)

	_, err := s.stdin.Write([]byte(full))
	if err != nil {
		stderr <- fmt.Sprintf("Could not send PowerShell command: %s\nError: %s", cmd, err.Error())
		close(stderr)
		close(stdout)
	}

	// read stdout and stderr
	sout := make(chan string)
	serr := make(chan string)

	waiter := &sync.WaitGroup{}
	waiter.Add(2)

	go streamReader(s.stdout, outBoundary, sout)
	go streamReader(s.stderr, errBoundary, serr)

	// read and write stdout
	go func() {
		for {
			stdoutLines, ok := <-sout
			if !ok {
				break
			}
			stdout <- stdoutLines
		}
		close(stdout)
		waiter.Done()
	}()
	// read and write the stderr
	go func() {
		for {
			stderrLines, ok := <-serr
			if !ok {
				break
			}
			stderr <- stderrLines
		}
		close(stderr)
		waiter.Done()
	}()
	waiter.Wait()
}

func (s *shell) Exit() {
	s.stdin.Write([]byte("exit" + newline))

	// if it's possible to close stdin, do so (some backends, like the local one,
	// do support it)
	closer, ok := s.stdin.(io.Closer)
	if ok {
		closer.Close()
	}

	s.handle.Wait()

	s.handle = nil
	s.stdin = nil
	s.stdout = nil
	s.stderr = nil
}

func streamReader(stream io.Reader, boundary string, output chan<- string) {

	// Read through the stream 1 line at a time
	for {
		outbytes, err := readLine(stream)
		if err != nil {
			break
		}
		line := string(outbytes)
		// we have a line if it is a boundry, end
		if line == boundary {
			close(output)
			break
		}
		output <- line
	}
}

func readLine(stream io.Reader) ([]byte, error) {
	buf := make([]byte, 1)
	var outbytes []byte
	for {
		_, err := stream.Read(buf)
		if err != nil {
			return nil, err
		}
		if buf[0] == 13 {
			// skip the '/r'
			continue
		}
		if buf[0] == 10 {
			return outbytes, nil
		}
		outbytes = append(outbytes, buf[0])
	}
}

func createBoundary() string {
	return "$gorilla" + utils.CreateRandomString(12) + "$"
}
