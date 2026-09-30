// Package ftn holds the FidoNet Technology Network building blocks the BBS
// uses to exchange echomail and netmail with other systems.
//
// It covers the wire formats: FTN addresses (ParseAddress), Type-2+ packets
// (ReadPacket, WritePacket and the kludge-aware message body helpers) and ZIP
// mail bundles (ExtractBundle, CreateBundle). It parses and downloads
// nodelists and echolists, and carries the embedded registry of known
// networks (LoadRegistry, LoadOverrideRegistry) behind the config editor's
// FTN setup wizard and network browser.
// The binkd files manage the binkd mailer's binkd.conf: generating it from
// the FTN configuration, and keeping node links, domains, outbounds and
// settings in sync with it (EnsureBinkdConf, SyncBinkdConf,
// SyncBinkdNetworks, SyncBinkdSettings, UpdateBinkdOwnAddress).
package ftn
