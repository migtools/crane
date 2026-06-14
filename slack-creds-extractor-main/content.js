// Injected into Slack page (MAIN world) via chrome.scripting.executeScript.
// Reads the xoxc token from localStorage and returns it.

(function () {
  try {
    const configRaw = localStorage.getItem("localConfig_v2");
    if (!configRaw) return null;

    const config = JSON.parse(configRaw);
    const teams = config.teams || {};

    for (const teamId of Object.keys(teams)) {
      const token = teams[teamId].token;
      if (token && token.startsWith("xoxc-")) {
        return token;
      }
    }
  } catch (e) {
    // ignore
  }
  return null;
})();
