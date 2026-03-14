//go:build unix

package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// unixController implements Controller for Unix systems (Linux, macOS, BSD).
type unixController struct {
	cmd  *exec.Cmd
	ptmx *os.File // PTY master
	opts *Options

	mu       sync.Mutex
	started  bool
	closed   bool
	done     chan struct{}
	waitErr  error
	exitCode int
}

// NewController creates a new PTY controller wrapping the given command.
// The command should not be started - NewController will start it.
func NewController(cmd *exec.Cmd, opts *Options) (Controller, error) {
	if cmd == nil {
		return nil, fmt.Errorf("cmd cannot be nil")
	}
	if opts == nil {
		opts = DefaultOptions()
	}

	return &unixController{
		cmd:  cmd,
		opts: opts,
	}, nil
}

// NewControllerFromArgs creates a new PTY controller for the given command and arguments.
func NewControllerFromArgs(name string, args []string, opts *Options) (Controller, error) {
	cmd := exec.Command(name, args...)
	return NewController(cmd, opts)
}

// Start begins execution of the wrapped command in a PTY.
func (c *unixController) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("controller already started")
	}
	if c.closed {
		return ErrClosed
	}

	// Apply options
	if c.opts.Dir != "" {
		c.cmd.Dir = c.opts.Dir
	}
	if len(c.opts.Env) > 0 {
		baseEnv := c.cmd.Env
		if len(baseEnv) == 0 {
			baseEnv = os.Environ()
		}
		c.cmd.Env = append(append([]string{}, baseEnv...), c.opts.Env...)
	}

	// Start the command with a PTY
	winSize := &pty.Winsize{
		Rows: c.opts.Rows,
		Cols: c.opts.Cols,
	}

	ptmx, tty, err := pty.Open()
	if err != nil {
		return fmt.Errorf("open pty: %w", err)
	}
	defer func() { _ = tty.Close() }()

	if err := pty.Setsize(ptmx, winSize); err != nil {
		_ = ptmx.Close()
		return fmt.Errorf("set pty size: %w", err)
	}

	if c.opts.DisableEcho {
		if err := disableTTYEcho(tty); err != nil {
			_ = ptmx.Close()
			return fmt.Errorf("disable tty echo: %w", err)
		}
	}

	if c.cmd.Stdout == nil {
		c.cmd.Stdout = tty
	}
	if c.cmd.Stderr == nil {
		c.cmd.Stderr = tty
	}
	if c.cmd.Stdin == nil {
		c.cmd.Stdin = tty
	}
	if c.cmd.SysProcAttr == nil {
		c.cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.cmd.SysProcAttr.Setsid = true
	c.cmd.SysProcAttr.Setctty = true

	if err := c.cmd.Start(); err != nil {
		_ = ptmx.Close()
		return fmt.Errorf("start pty: %w", err)
	}

	c.ptmx = ptmx
	c.started = true
	c.done = make(chan struct{})

	go c.waitForExit()

	return nil
}

func (c *unixController) waitForExit() {
	err := c.cmd.Wait()
	var exitErr *exec.ExitError

	c.mu.Lock()
	switch {
	case err == nil:
		c.exitCode = 0
		c.waitErr = nil
	case errors.As(err, &exitErr):
		c.exitCode = exitErr.ExitCode()
		c.waitErr = nil
	default:
		c.exitCode = -1
		c.waitErr = fmt.Errorf("wait: %w", err)
	}
	done := c.done
	c.mu.Unlock()

	close(done)
}

func waitDone(done chan struct{}) bool {
	if done == nil {
		return false
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// InjectCommand types a command into the PTY followed by a newline.
func (c *unixController) InjectCommand(cmd string) error {
	return c.InjectRaw([]byte(cmd + "\n"))
}

// InjectRaw writes raw bytes to the PTY.
func (c *unixController) InjectRaw(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return fmt.Errorf("controller not started")
	}
	if c.closed {
		return ErrClosed
	}
	if waitDone(c.done) {
		return ErrClosed
	}
	if ptyPeerClosed(c.ptmx) {
		return ErrClosed
	}

	_, err := c.ptmx.Write(data)
	if err != nil {
		if isClosedPTYWriteError(err) {
			return ErrClosed
		}
		return fmt.Errorf("write to pty: %w", err)
	}
	return nil
}

func isClosedPTYWriteError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrClosed) || errors.Is(err, os.ErrClosed) || errors.Is(err, io.ErrClosedPipe) {
		return true
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EIO || errno == syscall.EBADF
	}
	return false
}

func isClosedPTYReadError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
		return true
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EIO || errno == syscall.EBADF
	}
	return false
}

func ptyPeerClosed(ptmx *os.File) bool {
	if ptmx == nil {
		return true
	}
	pfds := []unix.PollFd{{
		Fd:     int32(ptmx.Fd()),
		Events: unix.POLLHUP | unix.POLLERR | unix.POLLNVAL,
	}}
	nready, err := unix.Poll(pfds, 0)
	if err != nil || nready == 0 {
		return false
	}
	return pfds[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0
}

// ReadOutput reads all available output from the PTY without blocking indefinitely.
func (c *unixController) ReadOutput() (string, error) {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return "", fmt.Errorf("controller not started")
	}
	if c.closed {
		c.mu.Unlock()
		return "", ErrClosed
	}
	ptmx := c.ptmx
	c.mu.Unlock()

	buf := make([]byte, 4096)
	nread, err := c.readWithTimeout(ptmx, buf, 100*time.Millisecond)
	if nread > 0 {
		return string(buf[:nread]), nil
	}

	if err != nil {
		if os.IsTimeout(err) {
			return "", nil // No data available within timeout
		}
		if err == io.EOF {
			return "", nil // Process exited
		}
		// Check for path error which wraps the syscall error
		if pathErr, ok := err.(*os.PathError); ok && pathErr.Timeout() {
			return "", nil
		}
		return "", fmt.Errorf("read from pty: %w", err)
	}
	return "", nil
}

// ReadLine reads a single line from the PTY output.
func (c *unixController) ReadLine(ctx context.Context) (string, error) {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return "", fmt.Errorf("controller not started")
	}
	if c.closed {
		c.mu.Unlock()
		return "", ErrClosed
	}
	ptmx := c.ptmx
	c.mu.Unlock()

	var line []byte
	buf := make([]byte, 1)

	for {
		// Check context cancellation first
		select {
		case <-ctx.Done():
			return string(line), ctx.Err()
		default:
		}

		nread, err := c.readWithTimeout(ptmx, buf, 100*time.Millisecond)
		if nread > 0 {
			line = append(line, buf[0])
			if buf[0] == '\n' {
				return string(line), nil
			}
		}

		if err != nil {
			if err == io.EOF {
				return string(line), io.EOF
			}
			if os.IsTimeout(err) {
				continue // Timeout, check context and retry
			}
			// Check for path error
			if pathErr, ok := err.(*os.PathError); ok && pathErr.Timeout() {
				continue
			}
			return string(line), fmt.Errorf("read from pty: %w", err)
		}
	}
}

// WaitForPattern reads output until the pattern matches or timeout.
func (c *unixController) WaitForPattern(ctx context.Context, pattern *regexp.Regexp, timeout time.Duration) (string, error) {
	if pattern == nil {
		return "", fmt.Errorf("pattern cannot be nil")
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return "", ErrClosed
	}
	ptmx := c.ptmx
	c.mu.Unlock()

	var output []byte
	buf := make([]byte, 4096)

	for {
		// Check context cancellation first
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return string(output), ErrTimeout
			}
			return string(output), ctx.Err()
		default:
		}

		nread, err := c.readWithTimeout(ptmx, buf, 100*time.Millisecond)
		if nread > 0 {
			output = append(output, buf[:nread]...)
			if pattern.Match(output) {
				return string(output), nil
			}
		}

		if err != nil {
			if err == io.EOF {
				return string(output), io.EOF
			}
			if os.IsTimeout(err) {
				continue // Timeout, check context and retry
			}
			// Check for path error
			if pathErr, ok := err.(*os.PathError); ok && pathErr.Timeout() {
				continue
			}
			return string(output), fmt.Errorf("read from pty: %w", err)
		}
	}
}

func (c *unixController) readWithTimeout(ptmx *os.File, buf []byte, timeout time.Duration) (int, error) {
	pollTimeoutMs := int(timeout / time.Millisecond)
	if pollTimeoutMs < 1 {
		pollTimeoutMs = 1
	}
	pfds := []unix.PollFd{{
		Fd:     int32(ptmx.Fd()),
		Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR | unix.POLLNVAL,
	}}
	for {
		nready, err := unix.Poll(pfds, pollTimeoutMs)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return 0, fmt.Errorf("poll pty: %w", err)
		}
		if nready == 0 {
			return 0, os.ErrDeadlineExceeded
		}
		revents := pfds[0].Revents
		if revents&unix.POLLNVAL != 0 {
			return 0, os.ErrClosed
		}
		if revents&(unix.POLLHUP|unix.POLLERR) != 0 && revents&unix.POLLIN == 0 {
			return 0, io.EOF
		}
		nread, err := ptmx.Read(buf)
		if err != nil && isClosedPTYReadError(err) {
			return nread, io.EOF
		}
		return nread, err
	}
}

// Wait waits for the command to exit and returns its exit code.
func (c *unixController) Wait() (int, error) {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return -1, fmt.Errorf("controller not started")
	}
	done := c.done
	c.mu.Unlock()

	<-done

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitCode, c.waitErr
}

// Signal sends a signal to the running process.
func (c *unixController) Signal(sig Signal) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return fmt.Errorf("controller not started")
	}
	if c.closed {
		return ErrClosed
	}
	if waitDone(c.done) {
		return ErrClosed
	}
	if c.cmd.Process == nil {
		return fmt.Errorf("process not running")
	}

	var s syscall.Signal
	switch sig {
	case SIGINT:
		s = syscall.SIGINT
	case SIGTERM:
		s = syscall.SIGTERM
	case SIGKILL:
		s = syscall.SIGKILL
	case SIGHUP:
		s = syscall.SIGHUP
	default:
		return fmt.Errorf("unknown signal: %d", sig)
	}

	return c.cmd.Process.Signal(s)
}

// Close terminates the PTY and cleans up resources.
func (c *unixController) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	ptmx := c.ptmx
	c.ptmx = nil
	cmd := c.cmd
	done := c.done
	c.mu.Unlock()

	var firstErr error

	// Close the PTY master (this will cause the child to receive SIGHUP)
	if ptmx != nil {
		if err := ptmx.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close pty: %w", err)
		}
	}

	// Kill the process if still running
	if cmd != nil && cmd.Process != nil {
		select {
		case <-done:
			return firstErr
		default:
		}

		// Try graceful termination first
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && firstErr == nil && !errors.Is(err, os.ErrProcessDone) {
			firstErr = fmt.Errorf("signal term: %w", err)
		}

		// Give it a moment to exit
		select {
		case <-done:
			// Process exited
		case <-time.After(100 * time.Millisecond):
			// Force kill
			if err := cmd.Process.Kill(); err != nil && firstErr == nil && !errors.Is(err, os.ErrProcessDone) {
				firstErr = fmt.Errorf("kill process: %w", err)
			}
			select {
			case <-done:
			case <-time.After(100 * time.Millisecond):
			}
		}
	}

	return firstErr
}

// Fd returns the file descriptor of the PTY master.
func (c *unixController) Fd() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ptmx == nil {
		return -1
	}
	return int(c.ptmx.Fd())
}
