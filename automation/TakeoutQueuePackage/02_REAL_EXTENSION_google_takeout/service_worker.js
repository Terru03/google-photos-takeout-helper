const MAX_ACTIVE = 5;
const RETRY_DELAY_MS = 30000;

const retrying = new Set();

function isTakeout(d) {
  const name = (d.filename || "").toLowerCase();
  const url = (d.url || "").toLowerCase();

  // REAL MODE:
  // Touch only Google Takeout-looking ZIP files or URLs.
  return name.includes("takeout-") || url.includes("takeout");
}

async function getTakeoutDownloads() {
  const all = await chrome.downloads.search({});
  return all.filter(isTakeout);
}

async function safePause(id) {
  try {
    await chrome.downloads.pause(id);
  } catch (err) {
    console.warn("Pause failed", id, err);
  }
}

async function safeResume(id) {
  try {
    await chrome.downloads.resume(id);
  } catch (err) {
    console.warn("Resume failed", id, err);
  }
}

async function manageQueue() {
  const takeout = await getTakeoutDownloads();

  const running = takeout.filter(d => d.state === "in_progress" && !d.paused);

  // Pause extra active downloads above the limit.
  if (running.length > MAX_ACTIVE) {
    const extra = running
      .sort((a, b) => new Date(b.startTime) - new Date(a.startTime))
      .slice(0, running.length - MAX_ACTIVE);

    for (const d of extra) {
      await safePause(d.id);
    }
  }

  let refreshed = await getTakeoutDownloads();
  let activeCount = refreshed.filter(d => d.state === "in_progress" && !d.paused).length;

  // Retry interrupted downloads first, if Chrome says they are resumable.
  const failedResumable = refreshed
    .filter(d => d.state === "interrupted" && d.canResume)
    .sort((a, b) => new Date(a.startTime) - new Date(b.startTime));

  for (const d of failedResumable) {
    if (activeCount >= MAX_ACTIVE) break;
    if (retrying.has(d.id)) continue;

    retrying.add(d.id);

    setTimeout(async () => {
      await safeResume(d.id);
      retrying.delete(d.id);
      manageQueue();
    }, RETRY_DELAY_MS);

    activeCount++;
  }

  refreshed = await getTakeoutDownloads();
  activeCount = refreshed.filter(d => d.state === "in_progress" && !d.paused).length;

  // Then resume normal paused downloads.
  const pausedNow = refreshed
    .filter(d => d.state === "in_progress" && d.paused)
    .sort((a, b) => new Date(a.startTime) - new Date(b.startTime));

  for (const d of pausedNow) {
    if (activeCount >= MAX_ACTIVE) break;
    await safeResume(d.id);
    activeCount++;
  }
}

chrome.downloads.onCreated.addListener(manageQueue);
chrome.downloads.onChanged.addListener(manageQueue);

chrome.alarms.create("takeoutQueueCheck", { periodInMinutes: 1 });
chrome.alarms.onAlarm.addListener(manageQueue);

manageQueue();
