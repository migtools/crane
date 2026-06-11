const NATIVE_HOST = "com.slack.token_saver";
const statusDiv = document.getElementById("status");
const extractBtn = document.getElementById("extract");

function showStatus(message, type) {
  statusDiv.textContent = message;
  statusDiv.className = type;
}

async function getDCookie() {
  // chrome.cookies.get is scoped to *.slack.com via host_permissions.
  // Only the 'd' cookie from slack.com is read — no other site's cookies.
  const cookie = await chrome.cookies.get({
    url: "https://slack.com",
    name: "d",
  });
  return cookie ? cookie.value : null;
}

async function getXoxcToken() {
  // Find a Slack tab to inject the localStorage reader into
  const tabs = await chrome.tabs.query({ url: "https://*.slack.com/*" });
  if (tabs.length === 0) {
    return null;
  }

  // Inject content.js into the MAIN world so it can access localStorage
  const results = await chrome.scripting.executeScript({
    target: { tabId: tabs[0].id },
    files: ["content.js"],
    world: "MAIN",
  });

  if (results && results[0] && results[0].result) {
    return results[0].result;
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
        reject(
          new Error(
            error.message.includes("not found")
              ? "Native host not installed. Run the install.sh script in the native-host/ directory."
              : error.message
          )
        );
      }
    });

    port.postMessage({
      xoxc_token: xoxcToken,
      d_cookie: dCookie,
    });
  });
}

extractBtn.addEventListener("click", async () => {
  extractBtn.disabled = true;
  showStatus("Extracting tokens...", "info");

  try {
    const dCookie = await getDCookie();
    if (!dCookie) {
      showStatus(
        "Could not find Slack 'd' cookie. Make sure you are logged into Slack in this browser.",
        "error"
      );
      extractBtn.disabled = false;
      return;
    }

    const xoxcToken = await getXoxcToken();
    if (!xoxcToken) {
      showStatus(
        "Could not find xoxc token. Make sure you have a Slack workspace tab open and try again.",
        "error"
      );
      extractBtn.disabled = false;
      return;
    }

    const message = await saveViaNativeHost(xoxcToken, dCookie);
    showStatus(message, "success");
  } catch (err) {
    showStatus(err.message, "error");
  } finally {
    extractBtn.disabled = false;
  }
});
