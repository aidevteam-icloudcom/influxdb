// Libraries
import React, {PureComponent} from 'react'
import {connect} from 'react-redux'
import {withRouter, WithRouterProps} from 'react-router'

// Components
import NoteEditor from 'src/dashboards/components/NoteEditor'
import {
  Button,
  ComponentColor,
  ComponentStatus,
  SpinnerContainer,
  TechnoSpinner,
  Overlay,
} from '@influxdata/clockface'

// Actions
import {
  createNoteCell,
  updateViewNote,
  loadNote,
  resetNoteState,
} from 'src/dashboards/actions/notes'
import {notify} from 'src/shared/actions/notifications'

// Utils
import {savingNoteFailed} from 'src/shared/copy/notifications'

// Types
import {RemoteDataState} from 'src/types'
import {AppState, NoteEditorMode} from 'src/types'

interface OwnProps {
  onDismiss: () => void
  cellID?: string
}

interface StateProps {
  mode: NoteEditorMode
  viewsStatus: RemoteDataState
}

interface DispatchProps {
  onCreateNoteCell: typeof createNoteCell
  onUpdateViewNote: typeof updateViewNote
  resetNote: typeof resetNoteState
  onNotify: typeof notify
  loadNote: typeof loadNote
}

interface RouterProps extends WithRouterProps {
  params: {
    dashboardID: string
  }
}

type Props = OwnProps & StateProps & DispatchProps & RouterProps

interface State {
  savingStatus: RemoteDataState
}

class NoteEditorOverlay extends PureComponent<Props, State> {
  public state: State = {
    savingStatus: RemoteDataState.NotStarted,
  }

  componentDidMount() {
    const {cellID} = this.props

    if (cellID) {
      this.props.loadNote(cellID)
    } else {
      this.props.resetNote()
    }
  }

  componentDidUpdate(prevProps: Props) {
    const {cellID, viewsStatus} = this.props

    if (
      prevProps.viewsStatus !== RemoteDataState.Done &&
      viewsStatus === RemoteDataState.Done
    ) {
      if (cellID) {
        this.props.loadNote(cellID)
      } else {
        this.props.resetNote()
      }
    }
  }

  public render() {
    const {
      onDismiss,
      params: {dashboardID},
    } = this.props

    if (!dashboardID) {
      return (
        <Overlay visible={true}>
          <Overlay.Container maxWidth={360}>
            <Overlay.Header title="Oh no!" onDismiss={onDismiss} />
            <Overlay.Body>
              <h5>
                This page does not allow creation or editing of notes, better
                head to a dashboard to do that.
              </h5>
            </Overlay.Body>
          </Overlay.Container>
        </Overlay>
      )
    }

    return (
      <Overlay visible={true}>
        <Overlay.Container maxWidth={900}>
          <Overlay.Header title={this.overlayTitle} onDismiss={onDismiss} />
          <Overlay.Body>
            <SpinnerContainer
              loading={this.props.viewsStatus}
              spinnerComponent={<TechnoSpinner />}
            >
              <NoteEditor />
            </SpinnerContainer>
          </Overlay.Body>
          <Overlay.Footer>
            <Button text="Cancel" onClick={onDismiss} />
            <Button
              text="Save"
              color={ComponentColor.Success}
              status={this.saveButtonStatus}
              onClick={this.handleSave}
            />
          </Overlay.Footer>
        </Overlay.Container>
      </Overlay>
    )
  }

  private get overlayTitle(): string {
    const {mode} = this.props

    let overlayTitle: string

    if (mode === NoteEditorMode.Editing) {
      overlayTitle = 'Edit Note'
    } else {
      overlayTitle = 'Add Note'
    }

    return overlayTitle
  }

  private get saveButtonStatus(): ComponentStatus {
    const {savingStatus} = this.state

    if (savingStatus === RemoteDataState.Loading) {
      return ComponentStatus.Loading
    }

    return ComponentStatus.Default
  }

  private handleSave = async () => {
    const {
      cellID,
      params: {dashboardID},
      onCreateNoteCell,
      onUpdateViewNote,
      onNotify,
      onDismiss,
    } = this.props

    this.setState({savingStatus: RemoteDataState.Loading})

    try {
      if (cellID) {
        await onUpdateViewNote(cellID)
      } else {
        await onCreateNoteCell(dashboardID)
      }
      onDismiss()
    } catch (error) {
      onNotify(savingNoteFailed(error.message))
      console.error(error)
      this.setState({savingStatus: RemoteDataState.Error})
    }
  }
}

const mstp = ({noteEditor, views}: AppState): StateProps => {
  const {mode} = noteEditor
  const {status} = views

  return {mode, viewsStatus: status}
}

const mdtp = {
  onNotify: notify,
  onCreateNoteCell: createNoteCell,
  onUpdateViewNote: updateViewNote,
  resetNote: resetNoteState,
  loadNote,
}

export default connect<StateProps, DispatchProps, OwnProps>(
  mstp,
  mdtp
)(withRouter<OwnProps>(NoteEditorOverlay))
