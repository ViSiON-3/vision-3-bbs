// Package qwk reads and writes QWK mail packets and their REP replies. It
// knows only the file formats; which areas and messages go into a packet is
// decided by its callers.
//
// It serves two roles. As a BBS offering offline mail, PacketWriter builds
// the QWK packet a caller downloads (CONTROL.DAT, MESSAGES.DAT, DOOR.ID and
// .NDX files) and ReadREP parses the REP packet they upload back. As a node
// on a QWK network (see qwknet), ReadPacket parses the packet fetched from a
// hub and WriteNetREP builds the node's outbound REP, carrying the
// HEADERS.DAT fields and Synchronet-style kludges that thread and route
// network mail (NetMessage).
package qwk
