/** Match JSON Schema maxLength and Go utf8.RuneCountInString. */
/** @param {string} value @param {number} limit */
export function withinCodePointLimit(value, limit) {
  return Array.from(value).length <= limit;
}
/** URL limits are stored/validated in UTF-8 bytes, independently of text fields. */
/** @param {string} value @param {number} limit */
export function withinByteLimit(value, limit) {
  return new TextEncoder().encode(value).length <= limit;
}
