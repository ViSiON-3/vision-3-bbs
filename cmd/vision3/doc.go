// Command vision3 is the ViSiON/3 BBS server. Run from the installation
// directory, it checks that setup has been done (configs, data, menus/v3,
// assets), loads configs/config.json, and brings up the enabled front ends:
// the SSH server (which also carries the WFC sysop admin console), the telnet
// server, and the experimental QWK packet API. Alongside them it starts the
// event scheduler, the V3Net networking service, the integrated binkd mailer,
// and a file watcher that hot-reloads configuration.
//
// Each caller is assigned the lowest free node number and handed to the menu
// executor. SIGHUP reloads configuration; SIGINT or SIGTERM shuts the server
// down cleanly. The --output-mode flag (auto, utf8, cp437) forces how
// characters are encoded for every session.
package main
