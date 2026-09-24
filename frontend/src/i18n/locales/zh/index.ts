import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import upstreamCenter from './upstreamCenter'
import intelligenceMonitor from './intelligenceMonitor'
import admin from './admin'
import misc from './misc'

export default {
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...upstreamCenter,
  ...intelligenceMonitor,
  admin,
  ...misc,
}
