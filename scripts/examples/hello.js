// hello.js — Vision/3 VPL Script Example
//
// Demonstrates v3.console, v3.session, v3.user, v3.users, and v3.data APIs.
// Configure in doors.json as:
//   { "name": "HELLO", "type": "v3_script", "script": "hello.js",
//     "working_directory": "scripts/examples" }

v3.console.clear();

v3.console.println("|09========================================");
v3.console.println("|11  Welcome to Vision/3 VPL Scripting!");
v3.console.println("|09========================================");
v3.console.println("");

// --- v3.session: BBS and session info ---
v3.console.println("|07BBS Name:     |15" + v3.session.bbs.name);
v3.console.println("|07SysOp:        |15" + v3.session.bbs.sysop);
v3.console.println("|07Version:      |15" + v3.session.bbs.version);
v3.console.println("|07Node:         |15" + v3.session.node);
v3.console.println("|07Time Left:    |15" + v3.session.timeLeft + "|07 seconds");
v3.console.println("");

// --- v3.user: current user info ---
v3.console.println("|07Hello, |15" + v3.user.handle + "|07!");
v3.console.println("|07Access Level: |15" + v3.user.accessLevel);
v3.console.println("|07Times Called: |15" + v3.user.timesCalled);
v3.console.println("|07Msgs Posted:  |15" + v3.user.messagesPosted);
v3.console.println("");

// --- v3.users: user database ---
v3.console.println("|07Total Users:  |15" + v3.users.count());
v3.console.println("");

// --- v3.data: persistent storage ---
var visitCount = v3.data.get("visitCount");
if (visitCount === undefined) {
    visitCount = 0;
}
visitCount++;
v3.data.set("visitCount", visitCount);

var lastVisitor = v3.data.get("lastVisitor");
if (lastVisitor) {
    v3.console.println("|07Last visitor: |15" + lastVisitor);
}
v3.console.println("|07This script has been visited |15" + visitCount + "|07 time(s).");
v3.data.set("lastVisitor", v3.user.handle);
v3.console.println("");

// --- Interactive demo ---
if (v3.console.yesno("|07Would you like to leave a message")) {
    v3.console.print("|07Enter your message: |15");
    var msg = v3.console.getstr(60);
    if (msg) {
        v3.console.println("");
        v3.console.println("|10You said: |15" + msg);
    }
}

v3.console.println("");
v3.console.println("|07Thanks for trying VPL scripting!");
v3.console.pause();
