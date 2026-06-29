/* ViSiON/3 BBS - Minimal site effects */

(function () {
    'use strict';

    /* Fade-in sections on scroll */
    var sections = document.querySelectorAll('section');

    if (!('IntersectionObserver' in window)) {
        sections.forEach(function (section) {
            section.classList.add('visible');
        });
        return;
    }

    var observer = new IntersectionObserver(function (entries) {
        entries.forEach(function (entry) {
            if (entry.isIntersecting) {
                entry.target.classList.add('visible');
            }
        });
    }, { threshold: 0.1 });

    sections.forEach(function (section) {
        section.classList.add('fade-in');
        observer.observe(section);
    });
})();

/* ---- Telix Dialer Splash Screen ---- */

// Cookie helpers
function hasVisitedBefore() {
    return document.cookie.includes('vision3_visited=1');
}

function setVisitedCookie() {
    var expires = new Date(Date.now() + 30 * 24 * 60 * 60 * 1000).toUTCString();
    document.cookie = 'vision3_visited=1; expires=' + expires + '; path=/; SameSite=Lax';
}

// DTMF frequency pairs (ITU-T Q.23)
var DTMF_FREQUENCIES = {
    '1': [697, 1209], '2': [697, 1336], '3': [697, 1477],
    '4': [770, 1209], '5': [770, 1336], '6': [770, 1477],
    '7': [852, 1209], '8': [852, 1336], '9': [852, 1477],
    '*': [941, 1209], '0': [941, 1336], '#': [941, 1477]
};

function playTone(audioContext, frequencies, duration, startTime) {
    frequencies.forEach(function (freq) {
        var oscillator = audioContext.createOscillator();
        var gainNode = audioContext.createGain();
        oscillator.type = 'sine';
        oscillator.frequency.value = freq;
        gainNode.gain.value = 0.15;
        oscillator.connect(gainNode);
        gainNode.connect(audioContext.destination);
        oscillator.start(startTime);
        oscillator.stop(startTime + duration);
    });
}

function playDialTone(audioContext, startTime, duration) {
    playTone(audioContext, [350, 440], duration, startTime);
}

function playRingTone(audioContext, startTime) {
    // US ring: 440+480 Hz, 2s on, 4s off
    playTone(audioContext, [440, 480], 2.0, startTime);
}

// Audio pool -- pre-unlocked on first user gesture
var audioPool = {
    pickupA: null,
    pickupB: null,
    modem: null,
    unlocked: false
};

function unlockAudioPool() {
    if (audioPool.unlocked) return;
    audioPool.unlocked = true;

    // Create two pickup instances (sequence uses it twice)
    audioPool.pickupA = new Audio('audio/phone-pickup.mp3');
    audioPool.pickupA.volume = 0.6;
    audioPool.pickupB = new Audio('audio/phone-pickup.mp3');
    audioPool.pickupB.volume = 0.6;

    audioPool.modem = new Audio('audio/modem-handshake.mp3');
    audioPool.modem.volume = 0.8;

    // iOS/Android unlock: play and immediately pause each element
    [audioPool.pickupA, audioPool.pickupB, audioPool.modem].forEach(function (el) {
        el.play().then(function () {
            el.pause();
            el.currentTime = 0;
        }).catch(function () {
            // silence errors on browsers that don't need this
        });
    });
}

var pickupIndex = 0;
function playPhonePickup() {
    var el = pickupIndex === 0 ? audioPool.pickupA : audioPool.pickupB;
    pickupIndex++;
    if (el) {
        el.currentTime = 0;
        el.play().catch(function (err) {
            console.error('Phone pickup click failed:', err);
        });
    }
}

// Typewriter text output
function typeText(terminal, text, callback) {
    var index = 0;
    var cursor = terminal.querySelector('.telix-cursor');
    function typeChar() {
        if (index < text.length) {
            cursor.insertAdjacentText('beforebegin', text[index]);
            index++;
            trackTimeout(typeChar, 30 + Math.random() * 20);
        } else if (callback) {
            callback();
        }
    }
    typeChar();
}

function printLine(terminal, text) {
    var cursor = terminal.querySelector('.telix-cursor');
    cursor.insertAdjacentText('beforebegin', text + '\n');
}

// Build Telix status bar with flexbox layout
function buildStatusBar(isOnline) {
    var statusBar = document.querySelector('.telix-status-bar');
    if (!statusBar) return;

    var isMobile = window.innerWidth <= 600;
    var leftContent = 'Unregistered';
    var middleContent = isMobile ? 'ANSI-BBS | 38400-N81' : '| ANSI-BBS | 38400-N81 FAX | | | |';
    var rightContent = isOnline ? 'Online 00:00' : 'Offline';

    statusBar.innerHTML =
        '<span>' + leftContent + '</span>' +
        '<span>' + middleContent + '</span>' +
        '<span>' + rightContent + '</span>';
}

// Active audio handles — tracked so skip can stop them
var activeAudioContext = null;
var activeModemAudio = null;

// Pending sequence timers — tracked so skip can cancel them all
var splashTimers = [];

// Document-level ESC handler — tracked so teardown can always detach it
var splashEscHandler = null;

function trackTimeout(fn, delay) {
    var id = setTimeout(fn, delay);
    splashTimers.push(id);
    return id;
}

function trackInterval(fn, delay) {
    var id = setInterval(fn, delay);
    splashTimers.push(id);
    return id;
}

function clearSplashTimers() {
    splashTimers.forEach(function (id) {
        clearTimeout(id);
        clearInterval(id);
    });
    splashTimers = [];
}

// Tear down the splash: cancel pending timers, stop audio, remove overlay.
// Safe to call once; subsequent calls are no-ops because the splash is gone.
function teardownSplash(splash) {
    clearSplashTimers();
    if (splashEscHandler) {
        document.removeEventListener('keydown', splashEscHandler);
        splashEscHandler = null;
    }
    if (activeModemAudio) {
        activeModemAudio.pause();
        activeModemAudio.currentTime = 0;
        activeModemAudio = null;
    }
    if (activeAudioContext) {
        activeAudioContext.close();
        activeAudioContext = null;
    }
    splash.remove();
    document.body.classList.remove('splash-active');
    setVisitedCookie();
}

function skipSplash(splash) {
    teardownSplash(splash);
}

// Main dialer sequence - chained callbacks for synchronous execution
function runDialerSequence(splash) {
    var terminal = document.getElementById('telix-terminal');
    var audioContext = new (window.AudioContext || window.webkitAudioContext)();
    activeAudioContext = audioContext;
    var phoneDigits = '13145673833';

    // Resume AudioContext if suspended (iOS Safari)
    if (audioContext.state === 'suspended') {
        audioContext.resume();
    }

    // Clear the "Click to connect..." prompt
    var prompt = terminal.querySelector('.telix-prompt');
    if (prompt) prompt.remove();

    var modemAudio = audioPool.modem;
    activeModemAudio = modemAudio;

    // Phase 1: Type AT&F, wait for OK
    typeText(terminal, 'AT&F\n', function () {
        printLine(terminal, 'OK');

        // Phase 2: Wait, then type init string
        trackTimeout(function () {
            typeText(terminal, 'AT&C1&D2&K3&M4&N6\n', function () {
                printLine(terminal, 'OK');

                // Phase 3: Wait, then type ATDT
                trackTimeout(function () {
                    var atdtChars = 'ATDT';
                    var atdtIndex = 0;
                    var cursor = terminal.querySelector('.telix-cursor');

                    var atdtInterval = trackInterval(function () {
                        if (atdtIndex < atdtChars.length) {
                            cursor.insertAdjacentText('beforebegin', atdtChars[atdtIndex]);
                            atdtIndex++;
                        } else {
                            clearInterval(atdtInterval);

                            // Click sound immediately after ATDT typed
                            playPhonePickup();

                            // Wait 0.5s, then start dial tone
                            trackTimeout(function () {
                                playDialTone(audioContext, audioContext.currentTime, 1.5);

                                // Phase 4: Type phone number with DTMF
                                trackTimeout(function () {
                                    var digitIndex = 0;
                                    var digitInterval = trackInterval(function () {
                                        if (digitIndex < phoneDigits.length) {
                                            var digit = phoneDigits[digitIndex];
                                            cursor.insertAdjacentText('beforebegin', digit);

                                            // Play DTMF for this digit
                                            var freqs = DTMF_FREQUENCIES[digit];
                                            if (freqs) {
                                                playTone(audioContext, freqs, 0.1, audioContext.currentTime);
                                            }
                                            digitIndex++;
                                        } else {
                                            clearInterval(digitInterval);
                                            cursor.insertAdjacentText('beforebegin', '\n');

                                            // Phase 5: First ring
                                            trackTimeout(function () {
                                                playRingTone(audioContext, audioContext.currentTime);

                                                // Phase 6: Wait 4.5s, then second ring
                                                trackTimeout(function () {
                                                    playRingTone(audioContext, audioContext.currentTime);

                                                    // Show RING text shortly after second ring starts
                                                    trackTimeout(function () {
                                                        printLine(terminal, 'RING');

                                                    // Phase 7: Wait 1.5s, phone pickup click
                                                    trackTimeout(function () {
                                                        playPhonePickup();

                                                        // Phase 8: Wait 0.5s, then modem screech (plays once, ~5s)
                                                        trackTimeout(function () {
                                                            // Play screech file once
                                                            if (modemAudio) {
                                                                modemAudio.currentTime = 0;
                                                                modemAudio.play().catch(function (err) {
                                                                    console.error('Modem audio playback failed:', err);
                                                                });
                                                            }

                                                            // Phase 9: CONNECT after screech finishes (~18.5s)
                                                            trackTimeout(function () {
                                                                printLine(terminal, 'CONNECT 14400/ARQ/V32bis/LAPM');

                                                                // Update status bar to ONLINE
                                                                buildStatusBar(true);

                                                                // Phase 10: Remove overlay after brief pause
                                                                trackTimeout(function () {
                                                                    teardownSplash(splash);
                                                                }, 2000);
                                                            }, 18500);
                                                        }, 500);
                                                    }, 1500);
                                                    }, 200);
                                                }, 4500);
                                        }, 1000);
                                    }
                                    }, 180);
                                }, 200);
                            }, 500);
                        }
                    }, 50);
                }, 300);
            });
        }, 300);
    });
}

// Initialize
(function () {
    var splash = document.getElementById('telix-splash');
    if (!splash) return;

    // Skip splash if user prefers reduced motion
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
        splash.remove();
        return;
    }

    if (hasVisitedBefore()) {
        splash.remove();
        return;
    }

    document.body.classList.add('splash-active');

    // Build status bar after page is fully rendered
    setTimeout(function () {
        buildStatusBar(false);
    }, 100);

    function startSequence() {
        splash.removeEventListener('click', clickHandler);
        splash.style.cursor = 'default';
        unlockAudioPool();
        runDialerSequence(splash);
    }

    function clickHandler() {
        startSequence();
    }

    splash.addEventListener('click', clickHandler);

    var skipBtn = document.getElementById('telix-skip');
    if (skipBtn) {
        skipBtn.addEventListener('click', function (e) {
            e.stopPropagation(); // don't trigger splash clickHandler
            skipSplash(splash);
        });
        // Keep Enter/Space on the button from bubbling to the splash's
        // keyHandler and starting the modem sequence — the native button
        // click already fires skipSplash.
        skipBtn.addEventListener('keydown', function (e) {
            if (e.key === 'Enter' || e.key === ' ') {
                e.stopPropagation();
            }
        });
    }

    // Tracked so teardownSplash detaches it on skip OR normal completion,
    // not just when ESC is pressed.
    splashEscHandler = function (e) {
        if (e.key === 'Escape') {
            skipSplash(splash);
        }
    };
    document.addEventListener('keydown', splashEscHandler);
})();

/* ---- BBS Menu Keyboard Navigation ---- */
(function () {
    'use strict';

    var anchorMap = {
        'f': '#features',
        'F': '#features',
        'g': '#get-started',
        'G': '#get-started',
        'j': '#get-involved',
        'J': '#get-involved',
        'h': '#history',
        'H': '#history'
    };

    var hrefMap = {
        'd': '/sysop/',
        'D': '/sysop/'
    };

    document.addEventListener('keydown', function (e) {
        // Don't hijack if command/control modifiers are pressed or user is
        // typing. Shift is allowed so Shift+F (and Caps Lock) still navigate.
        if (e.ctrlKey || e.metaKey || e.altKey) {
            return;
        }

        if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA' ||
            e.target.isContentEditable || e.target.matches('[contenteditable="true"]')) {
            return;
        }

        if (hrefMap[e.key]) {
            e.preventDefault();
            window.location.href = hrefMap[e.key];
            return;
        }

        var section = anchorMap[e.key];
        if (section) {
            e.preventDefault();
            var element = document.querySelector(section);
            if (element) {
                element.scrollIntoView({ behavior: 'smooth' });
            }
        }
    });
})();
