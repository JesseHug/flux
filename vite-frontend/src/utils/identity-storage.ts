// Keep existing preferences and let a previous frontend build read changes
// made during the transition. Callers retain their existing storage error handling.
const legacyKeys = (key: string): string[] => {
  if (/^flux([:_])/.test(key)) {
    return [
      key.replace(/^flux([:_])/, "flyx$1"),
      key.replace(/^flux([:_])/, "flvx$1"),
    ];
  }

  return [];
};

export function getStoredValue(storage: Storage, key: string): string | null {
  const value = storage.getItem(key);

  if (value !== null) return value;

  for (const legacyKey of legacyKeys(key)) {
    const legacy = storage.getItem(legacyKey);

    if (legacy !== null) {
      try {
        storage.setItem(key, legacy);
      } catch {
        // Reading a saved preference must work even when storage is full.
      }

      return legacy;
    }
  }

  return null;
}

export function setStoredValue(storage: Storage, key: string, value: string) {
  storage.setItem(key, value);
  for (const legacyKey of legacyKeys(key)) {
    storage.setItem(legacyKey, value);
  }
}

export function removeStoredValue(storage: Storage, key: string) {
  storage.removeItem(key);
  for (const legacyKey of legacyKeys(key)) {
    storage.removeItem(legacyKey);
  }
}
