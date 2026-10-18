/** IndexedDB retains scoped descriptors and at most one bounded wire command per job.
 * File handles, authentication material and whole file bytes are never persisted.
 */
const DATABASE = "traccia-attachment-jobs-v1";
function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DATABASE, 2);
    request.onupgradeneeded = () => {
      for (const name of ["jobs", "removals"])
        if (!request.result.objectStoreNames.contains(name))
          request.result.createObjectStore(name, { keyPath: "id" });
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}
async function transaction<T>(
  table: string,
  mode: IDBTransactionMode,
  run: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
  const db = await open();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(table, mode),
      request = run(tx.objectStore(table));
    let result: T;
    request.onsuccess = () => {
      result = request.result;
    };
    tx.oncomplete = () => {
      db.close();
      resolve(result);
    };
    tx.onerror = () => {
      db.close();
      reject(tx.error);
    };
    tx.onabort = () => {
      db.close();
      reject(tx.error);
    };
  });
}
function table(name: string) {
  return {
    read: () =>
      transaction<unknown[]>(name, "readonly", (store) => store.getAll()),
    put: (value: { id: string }) =>
      transaction(name, "readwrite", (store) => store.put(value)),
    remove: (id: string) =>
      transaction(name, "readwrite", (store) => store.delete(id)),
  };
}
export const attachmentPersistence = table("jobs");
export const attachmentRemovalPersistence = table("removals");
