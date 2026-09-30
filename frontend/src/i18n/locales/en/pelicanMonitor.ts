export default {
  pelicanMonitor: {
    title: 'Pelican monitor', description: 'Follow each group’s generation results with the same drawing prompt.', notice: 'Notice',
    rate: 'Group multiplier', schedule: 'Schedule', everyMinutes: 'Every {count} minutes', everySeconds: 'Every {count} seconds', next: 'Next check', afterCurrent: 'After this run', waitingSchedule: 'Awaiting schedule', paused: 'Schedule paused', lastRun: 'Last check',
    recentWorks: 'Recent artwork', newestFirst: 'Newest first', latest: 'Latest', duration: 'Generation time', search: 'Search group names', groupCount: '{count} groups', updated: 'Updated {seconds}s ago',
    status: { pending: 'Waiting', running: 'Drawing', succeeded: 'Completed', failed: 'Generation failed', idle: 'Not checked yet' },
    refresh: 'Refresh', preview: 'Pelican animation preview', open: 'View artwork', reloadPreview: 'Replay', loadingPreview: 'Loading artwork…', loadFailed: 'Unable to load. Please refresh and try again.', noHTML: 'No artwork available for this run', waiting: 'Waiting for the first artwork',
    disabled: 'Pelican monitoring is not available', disabledHint: 'Latest group artwork will appear here when monitoring is opened.', empty: 'No monitoring groups are published yet', noMatches: 'No matching groups', retention: 'Each group keeps its latest 20 checks. Artwork plays automatically, new results update live, and clicking opens the full view.',
  },
}
