// Background service worker — automatically extracts and saves
// Slack tokens every 6 hours without user interaction.

const NATIVE_HOST = "com.slack.token_saver";
const ALARM_NAME = "slack-token-refresh";
const REFRESH_INTERVAL_MINUTES = 360; // 6 hours

async function getDCookie() {
  const cookie = await chrome.cookies.get({
    url: "https://slack.com",
    name: "d",
  });
  return cookie ? cookie.value : null;
}

async function getXoxcToken() {
  const tabs = await chrome.tabs.query({ url: "https://*.slack.com/*" });
  if (tabs.length === 0) {
    return null;
  }

  try {
    const results = await chrome.scripting.executeScript({
      target: { tabId: tabs[0].id },
      files: ["content.js"],
      world: "MAIN",
    });

    if (results && results[0] && results[0].result) {
      return results[0].result;
    }
  } catch (e) {
    // Tab may have been closed or navigated away
  }
  return null;
}

function saveViaNativeHost(xoxcToken, dCookie) {
  return new Promise((resolve, reject) => {
    let settled = false;
    const port = chrome.runtime.connectNative(NATIVE_HOST);

    port.onMessage.addListener((response) => {
      if (settled) return;
      settled = true;
      port.disconnect();
      if (response.success) {
        resolve(response.message);
      } else {
        reject(new Error(response.message));
      }
    });

    port.onDisconnect.addListener(() => {
      if (settled) return;
      settled = true;
      const error = chrome.runtime.lastError;
      if (error) {
        reject(new Error(error.message));
      }
    });

    port.postMessage({
      xoxc_token: xoxcToken,
      d_cookie: dCookie,
    });
  });
}

async function extractAndSave() {
  const dCookie = await getDCookie();
  if (!dCookie) {
    console.log("Slack Token Extractor: No d cookie found, skipping");
    return;
  }

  const xoxcToken = await getXoxcToken();
  if (!xoxcToken) {
    console.log(
      "Slack Token Extractor: No Slack tab open, skipping xoxc token refresh"
    );
    return;
  }

  try {
    const message = await saveViaNativeHost(xoxcToken, dCookie);
    console.log("Slack Token Extractor:", message);
  } catch (e) {
    console.error("Slack Token Extractor: Failed to save tokens:", e.message);
  }
}

// Run on extension install/update
chrome.runtime.onInstalled.addListener(() => {
  chrome.alarms.create(ALARM_NAME, {
    delayInMinutes: 1,
    periodInMinutes: REFRESH_INTERVAL_MINUTES,
  });
  console.log(
    "Slack Token Extractor: Alarm set, refreshing every",
    REFRESH_INTERVAL_MINUTES,
    "minutes"
  );
});

// Run on browser startup
chrome.runtime.onStartup.addListener(() => {
  chrome.alarms.create(ALARM_NAME, {
    delayInMinutes: 1,
    periodInMinutes: REFRESH_INTERVAL_MINUTES,
  });
});

// Handle alarm
chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === ALARM_NAME) {
    extractAndSave();
  }
});
