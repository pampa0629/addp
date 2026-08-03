export function createLatestRequestCoordinator() {
  let latestRequestID = 0

  return {
    begin(targetValue) {
      return {
        id: ++latestRequestID,
        targetValue
      }
    },
    invalidate() {
      latestRequestID += 1
    },
    isCurrent(request, currentTargetValue) {
      return request.id === latestRequestID && request.targetValue === currentTargetValue
    }
  }
}
