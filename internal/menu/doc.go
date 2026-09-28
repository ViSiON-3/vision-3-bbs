// Package menu drives a caller's session once they are connected: it loads
// ViSiON/2-style menus and runs them, and it implements the built-in commands
// those menus invoke.
//
// A menu is a .MNU record (MenuRecord) plus a command list (CommandRecord),
// read from the active menu set by LoadMenu and LoadCommands. MenuExecutor,
// built once at startup by NewExecutor, holds the shared managers and the
// hot-reloadable configuration; the server calls its RunChallengeGate,
// RunMatrixScreen, RunLoginSequence and Run methods for each session. Run
// displays a menu, reads the caller's input and dispatches the matched
// command: GOTO switches menus, LOGOFF ends the session, DOOR launches an
// external door, and RUN calls a RunnableFunc from RunRegistry. Those
// runnables make up most of the package: login and new-user flows, message
// and file areas, private mail, doors, chat, oneliners, voting, V3Net and
// sysop administration screens. Access to menus and commands is gated by ACS
// strings, which CheckUserACS evaluates outside a running menu.
package menu
