// Libraries
import React, {FunctionComponent} from 'react'
import {connect} from 'react-redux'

// Types
import {AppState} from 'src/types'

// Components
import {Overlay} from '@influxdata/clockface'
import NoteEditorOverlay from 'src/dashboards/components/NoteEditorOverlay'

interface StateProps {
  overlayID: string
}

const OverlayController: FunctionComponent<StateProps> = ({overlayID}) => {
  let activeOverlay = <></>

  switch (overlayID) {
    case 'add-note':
    case 'edit-note':
      activeOverlay = <NoteEditorOverlay />
      break
  }

  return <Overlay visible={!!overlayID}>{activeOverlay}</Overlay>
}

const mstp = ({overlays: {id}}: AppState): StateProps => ({overlayID: id})

export default connect<StateProps, {}, {}>(mstp)(OverlayController)
