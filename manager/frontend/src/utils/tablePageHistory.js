// The browser retains only page entry cursors. Each visit reads and authorizes the page again.
export function requestTablePage(history, locator, size, page) {
  if (history.locator !== locator || history.size !== size) {
    Object.assign(history, { locator, size, positions: {} })
  }
  const cursor = history.positions?.[page]
  return cursor ? { page_cursor: cursor } : {}
}

export function rememberTablePage(history, page, data) {
  if (!data?.sequential) {
    history.positions = {}
    return
  }
  for (const visited of Object.keys(history.positions)) {
    if (Number(visited) > page) delete history.positions[visited]
  }
  if (data.next_page_cursor) history.positions[page + 1] = data.next_page_cursor
}
