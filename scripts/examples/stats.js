// stats.js — System Statistics Display
//
// Shows BBS statistics: users, messages, files, nodes.
// Pauses when output exceeds the user's terminal height.
//
// Configure in doors.json:
//   { "name": "STATS", "type": "v3_script", "script": "stats.js",
//     "working_directory": "scripts/examples" }

var height = v3.console.height || 24;
var lineCount = 0;

// println with automatic "more" pause when the screen is full.
function pl(s) {
    v3.console.println(s);
    lineCount++;
    if (lineCount >= height - 2) {
        v3.console.pause();
        lineCount = 0;
    }
}

v3.console.clear();
pl("|09=== |15System Statistics |09===");
pl("");

// BBS Info
pl("|11BBS Information");
pl("|07" + v3.util.padRight("", 40, "-"));
pl("|07Board Name:    |15" + v3.session.bbs.name);
pl("|07SysOp:         |15" + v3.session.bbs.sysop);
pl("|07Version:       |15" + v3.session.bbs.version);
pl("|07Current Date:  |15" + v3.util.date("01/02/2006"));
pl("|07Current Time:  |15" + v3.util.date("15:04:05"));
pl("");

// User Stats
pl("|11User Statistics");
pl("|07" + v3.util.padRight("", 40, "-"));
pl("|07Total Users:   |15" + v3.users.count());

if (v3.nodes) {
    pl("|07Nodes Online:  |15" + v3.nodes.count());
}
pl("");

// Message Stats
if (v3.message) {
    pl("|11Message Statistics");
    pl("|07" + v3.util.padRight("", 40, "-"));
    pl("|07Total Messages:|15 " + v3.message.totalCount());
    var areas = v3.message.areas();
    pl("|07Message Areas: |15" + areas.length);
    pl("");
}

// File Stats
if (v3.file) {
    pl("|11File Statistics");
    pl("|07" + v3.util.padRight("", 40, "-"));
    pl("|07Total Files:   |15" + v3.file.totalCount());
    var fileAreas = v3.file.areas();
    pl("|07File Areas:    |15" + fileAreas.length);
    pl("");
}

// Current User
pl("|11Your Session");
pl("|07" + v3.util.padRight("", 40, "-"));
pl("|07Handle:        |15" + v3.user.handle);
pl("|07Access Level:  |15" + v3.user.accessLevel);
pl("|07Times Called:  |15" + v3.user.timesCalled);
pl("|07Node:          |15" + v3.session.node);
pl("|07Time Left:     |15" + v3.session.timeLeft + " seconds");

v3.console.pause();
