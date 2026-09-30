// Management display keeps history visible without changing effective revision resolution.
export const getStandardDisplayRevision = aggregate => aggregate?.draft_revision || aggregate?.current_revision || aggregate?.latest_revision
