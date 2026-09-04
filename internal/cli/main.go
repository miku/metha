package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/miku/metha"
	"github.com/miku/metha/oai"
)

// Main is the whole of every main package metha ships. The binary decides what
// to do from the name it was invoked under, so cmd/metha and the nine legacy
// cmd/metha-* stubs are the same three lines.
func Main() {
	ctx := interruptible()
	// Every command, before any of them can make a request. Commands that
	// choose a log destination of their own repeat this with that destination.
	routeStdlibLog(os.Stderr)
	setUserAgent()
	args, legacy := Dispatch(os.Args)
	root := NewRoot()
	if legacy != "" {
		LegacyNotice(os.Stderr, legacy, StderrIsTerminal())
	}
	root.SetArgs(RewriteArgs(root, args, legacy != ""))
	if err := root.ExecuteContext(ctx); err != nil {
		reportError(os.Stderr, err)
		os.Exit(1)
	}
}

// setUserAgent tells the endpoints which build is calling. The version is
// injected into the metha package by the release build (-X
// github.com/miku/metha.Version=...), and oai reads DefaultUserAgent on every
// request, so one assignment before the first of them covers the whole run, no
// matter which client a command builds.
//
// This is the program's business, not the library's: it used to be an init in
// package metha, which meant that importing metha for a type alias silently
// rewrote a global in oai and left some other binary claiming to be metha.
func setUserAgent() { oai.DefaultUserAgent = "metha/" + metha.Version }

// interruptible returns the context every command runs under: the first
// interrupt cancels it, and a long-running command stops at the next point it
// can leave its work consistent. For a harvest that is between two requests -
// the window in flight is dropped and refetched next run, which is the recovery
// path the writer already has.
//
// The second interrupt is deliberately left to the default action, which is to
// die immediately. signal.NotifyContext on its own would swallow it, and an
// operator holding Ctrl-C is asking to stop now, not to be told the shutdown is
// already in progress; releasing the handler is how they get what they asked
// for. What it costs is the shard's torn tail, which the next open truncates.
func interruptible() context.Context {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx
}
