**V3Net for Vision/3 — Why We're Building This**

Vision/3 already supports FTN echomail for sysops who want FidoNet, fsxNet, and similar networks — and that's not going anywhere. V3Net is something different: a native message networking protocol built specifically for the modern BBS revival, designed around how sysops actually run boards today.

**The FTN problem isn't the networks — it's the plumbing.** Getting on a FTN network means setting up a mailer (BinkD, etc.) as a separate process, obtaining a node number, managing nodelist updates, understanding packet routing, and staying current with tosser configuration. It's a weekend of work before a single message moves. V3Net reduces that to a URL, a token, and a config block. Five minutes.

**Real-time is genuinely new territory.** FTN echomail is store-and-forward by design — messages propagate on poll cycles measured in hours. V3Net has first-class real-time events baked into the protocol from the start: network-wide who's-on lists, logon notifications, and inter-BBS chat. No FTN network can do this.

**Inter-BBS chat as a native feature.** The SSE event stream that powers logon/logoff notifications is the same infrastructure that enables live chat between boards. A user on one Vision/3 system can exchange messages in real time with a user on any other node connected to the same V3Net network — no separate chat protocol, no IRC bridge, no third-party service. It's built in. This opens the door to network-wide one-liner walls, inter-BBS pages, and eventually multi-node chat rooms that span multiple boards as naturally as a local multi-line chat does today.

**File distribution as a natural extension.** The same hub/leaf polling architecture that moves messages can move files. A future V3Net file layer could allow hubs to announce new file additions — new uploads, new door game packages, new ANSI art packs — and leaf nodes to automatically mirror or selectively pull them. Think of it as a BBS-native CDN: decentralized, sysop-controlled, no cloud dependency. File areas on different boards stay in sync the same way message bases do today.

**Decentralized moderation.** Any Vision/3 sysop can host a hub for any named network. There's no central authority, no coordinator who controls access, no nodelist gatekeeper. If you don't like how a hub is run, you stand up your own and other sysops subscribe to yours. Networks are defined by the community that uses them, not by whoever controls a master nodelist.

**It stays out of the way of FTN.** V3Net runs alongside echomail, not instead of it. A board can be on FidoNet, fsxNet, and felonynet simultaneously. Each serves a different audience and use case.

**felonynet is the proof of concept** — a general-purpose public network that ships as the first named V3Net network, giving Vision/3 sysops something to join on day one without needing to coordinate anything.