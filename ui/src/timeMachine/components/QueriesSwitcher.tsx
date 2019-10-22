// Libraries
import React, {PureComponent} from 'react'
import {connect} from 'react-redux'

// Components
import {
  Button,
  ConfirmationButton,
  PopoverType,
  ComponentColor,
} from '@influxdata/clockface'

// Actions
import {
  editActiveQueryWithBuilder,
  editActiveQueryAsFlux,
} from 'src/timeMachine/actions'

// Utils
import {getActiveQuery} from 'src/timeMachine/selectors'
import {hasQueryBeenEdited} from 'src/timeMachine/utils/queryBuilder'

// Types
import {AppState, DashboardQuery} from 'src/types'

interface StateProps {
  activeQuery: DashboardQuery
}

interface DispatchProps {
  onEditWithBuilder: typeof editActiveQueryWithBuilder
  onEditAsFlux: typeof editActiveQueryAsFlux
}

type Props = StateProps & DispatchProps

class TimeMachineQueriesSwitcher extends PureComponent<Props> {
  public render() {
    const {onEditAsFlux, onEditWithBuilder} = this.props
    const {editMode, text, builderConfig} = this.props.activeQuery

    let button = (
      <Button
        text="Script Editor"
        titleText="Switch to Script Editor"
        onClick={onEditAsFlux}
        testID="switch-to-script-editor"
      />
    )
    
    if (editMode !== 'builder') {
      button = (
        <Button
        text="Query Builder"
        titleText="Switch to Query Builder"
        onClick={onEditWithBuilder}
        testID="switch-to-query-builder"
        />
      )
    }

    if (editMode !== 'builder' && hasQueryBeenEdited(text, builderConfig)) {
      button = (
        <ConfirmationButton
          popoverColor={ComponentColor.Danger}
          popoverType={PopoverType.Outline}
          confirmationLabel="Switching to Query Builder mode will discard any changes you
                have made using Flux. This cannot be recovered."
          confirmationButtonText="Switch to Builder"
          text="Query Builder"
          onConfirm={onEditWithBuilder}
          style={{width: '400px'}}
          testID="switch-query-builder-confirm"
        />
      )
    }

    return button
  }
}

const mstp = (state: AppState) => {
  const activeQuery = getActiveQuery(state)

  return {activeQuery}
}

const mdtp = {
  onEditWithBuilder: editActiveQueryWithBuilder,
  onEditAsFlux: editActiveQueryAsFlux,
}

export default connect<StateProps, DispatchProps>(
  mstp,
  mdtp
)(TimeMachineQueriesSwitcher)
