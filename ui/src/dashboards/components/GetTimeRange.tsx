// Libraries
import React, {FC, useEffect} from 'react'
import {connect, ConnectedProps} from 'react-redux'
import {withRouter, RouteComponentProps} from 'react-router-dom'
import {getTimeRange} from 'src/dashboards/selectors'

// Actions
import * as actions from 'src/dashboards/actions/ranges'

// Types
import {AppState} from 'src/types'

type ReduxProps = ConnectedProps<typeof connector>
type Props = RouteComponentProps<{dashboardID: string}> & ReduxProps

const GetTimeRange: FC<Props> = ({
  location,
  match,
  timeRange,
  setDashboardTimeRange,
  updateQueryParams,
}: Props) => {
  const isEditing = location.pathname.includes('edit')
  const isNew = location.pathname.includes('new')

  useEffect(() => {
    if (isEditing || isNew) {
      return
    }

    // TODO: map this to current contextID
    setDashboardTimeRange(match.params.dashboardID, timeRange)
    const {lower, upper} = timeRange
    updateQueryParams({
      lower,
      upper,
    })
  }, [isEditing, isNew])

  return <div />
}

const mstp = (state: AppState) => {
  const timeRange = getTimeRange(state)
  return {timeRange}
}

const mdtp = {
  updateQueryParams: actions.updateQueryParams,
  setDashboardTimeRange: actions.setDashboardTimeRange,
}

const connector = connect(mstp, mdtp)

export default withRouter(connector(GetTimeRange))
