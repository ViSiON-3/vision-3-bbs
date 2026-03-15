// last10.js — Last 10 Callers
//
// Displays the most recent callers to the BBS.
//
// Configure in doors.json:
//   { "name": "LAST10", "type": "v3_script", "script": "last10.js",
//     "working_directory": "scripts/examples" }

v3.console.clear();
v3.console.println("|09=== |15Last 10 Callers |09===");
v3.console.println("");
v3.console.println("|07" + v3.util.padRight("Handle", 20) + v3.util.padRight("Location", 24) + v3.util.padLeft("Last Login", 16));
v3.console.println("|09" + v3.util.padRight("", 60, "-"));

var allUsers = v3.users.list();

// Sort by lastLogin descending.
for (var i = 0; i < allUsers.length; i++) {
    for (var j = i + 1; j < allUsers.length; j++) {
        if (allUsers[j].lastLogin > allUsers[i].lastLogin) {
            var tmp = allUsers[i];
            allUsers[i] = allUsers[j];
            allUsers[j] = tmp;
        }
    }
}

var count = Math.min(allUsers.length, 10);
for (var k = 0; k < count; k++) {
    var u = allUsers[k];
    // Convert Unix timestamp to readable date.
    var d = new Date(u.lastLogin * 1000);
    var dateStr = (d.getMonth() + 1) + "/" + d.getDate() + "/" + d.getFullYear();

    v3.console.println(
        "|15" + v3.util.padRight(u.handle, 20) +
        "|07" + v3.util.padRight(u.location || "", 24) +
        "|03" + v3.util.padLeft(dateStr, 16)
    );
}

v3.console.println("");
v3.console.pause();
