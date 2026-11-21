type UpdateSWFn = (reloadPage?: boolean) => Promise<void>;

let swRegistration: ServiceWorkerRegistration | undefined;
let updateSWFn: UpdateSWFn | undefined;

export function registerPanelUpdater(
  registration: ServiceWorkerRegistration | undefined,
  updateSW: UpdateSWFn,
): void {
  swRegistration = registration;
  updateSWFn = updateSW;
}

function waitForWaitingSW(
  registration: ServiceWorkerRegistration,
  timeoutMs = 10_000,
): Promise<boolean> {
  if (registration.waiting) {
    return Promise.resolve(true);
  }
  const installing = registration.installing;

  if (!installing) {
    return Promise.resolve(false);
  }

  return new Promise((resolve) => {
    const timer = window.setTimeout(() => {
      installing.removeEventListener("statechange", onStateChange);
      resolve(Boolean(registration.waiting));
    }, timeoutMs);

    const onStateChange = () => {
      if (registration.waiting || installing.state === "installed") {
        window.clearTimeout(timer);
        installing.removeEventListener("statechange", onStateChange);
        resolve(Boolean(registration.waiting));
      } else if (installing.state === "redundant") {
        window.clearTimeout(timer);
        installing.removeEventListener("statechange", onStateChange);
        resolve(false);
      }
    };

    installing.addEventListener("statechange", onStateChange);
  });
}

async function clearBrowserCaches(): Promise<void> {
  if (typeof caches === "undefined") {
    return;
  }
  const keys = await caches.keys();

  await Promise.all(keys.map((key) => caches.delete(key)));
}

/**
 * Checks for a new Service Worker immediately; if none is found (or SW is
 * inactive), clears cached assets and forces a full page reload so panel
 * upgrades take effect right away without waiting for the background prompt.
 */
export async function forceRefreshPanelCache(): Promise<"sw" | "reload"> {
  if (typeof navigator !== "undefined" && !navigator.onLine) {
    throw new Error("当前处于离线状态，无法刷新面板缓存");
  }

  let reg = swRegistration;

  if (
    !reg &&
    typeof navigator !== "undefined" &&
    "serviceWorker" in navigator
  ) {
    reg = await navigator.serviceWorker.getRegistration();
  }

  if (reg) {
    if (reg.waiting && updateSWFn) {
      await updateSWFn(true);

      return "sw";
    }
    try {
      await reg.update();
    } catch {
      // Fall through to cache reset + hard reload.
    }
    if ((await waitForWaitingSW(reg)) && updateSWFn) {
      await updateSWFn(true);

      return "sw";
    }
    try {
      await reg.unregister();
    } catch {
      // Ignore unregister errors and reload anyway.
    }
  }

  await clearBrowserCaches().catch(() => {});
  window.location.reload();

  return "reload";
}
