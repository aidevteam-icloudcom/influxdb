// Libraries
import React, {FC, useRef, useEffect, ReactNode, useState} from 'react'
import {round} from 'lodash'
import classnames from 'classnames'

// Components
import ResultsHeader from 'src/notebooks/pipes/Query/ResultsHeader'

// Types
import {ResultsVisibility} from 'src/notebooks/pipes/Query'

interface Props {
  height: number
  onUpdateHeight: (height: number) => void
  children: ReactNode
  visibility: ResultsVisibility
  onUpdateVisibility: (visibility: ResultsVisibility) => void
  resizingEnabled: boolean
}

const MINIMUM_RESULTS_PANEL_HEIGHT = 100

const ResultsResizer: FC<Props> = ({
  height,
  onUpdateHeight,
  children,
  visibility,
  onUpdateVisibility,
  resizingEnabled,
}) => {
  const [size, updateSize] = useState<number>(height)
  const [isDragging, updateDragging] = useState<boolean>(false)
  const resultsBodyRef = useRef<HTMLDivElement>(null)
  const dragHandleRef = useRef<HTMLDivElement>(null)

  const resultsBodyClassName = classnames('notebook-raw-data--body', {
    [`notebook-raw-data--body__${visibility}`]: resizingEnabled && visibility,
  })

  const updateResultsStyle = (): void => {
    if (resultsBodyRef.current && resizingEnabled && visibility === 'visible') {
      resultsBodyRef.current.setAttribute('style', `height: ${size}px`)
    } else {
      resultsBodyRef.current.setAttribute('style', '')
    }
  }

  // Ensure results renders with proper height on initial render
  useEffect(() => {
    updateResultsStyle()
  }, [])

  // Update results height when associated props change
  useEffect(() => {
    updateResultsStyle()
  }, [size, visibility, resizingEnabled])

  // Update local height when context height changes
  // so long as it is a different value
  useEffect(() => {
    if (height !== size) {
      updateSize(height)
    }
  }, [height])

  // Handle changes in drag state
  useEffect(() => {
    if (isDragging === true) {
      dragHandleRef.current &&
        dragHandleRef.current.classList.add(
          'notebook-raw-data--drag-handle__dragging'
        )
    }

    if (isDragging === false) {
      dragHandleRef.current &&
        dragHandleRef.current.classList.remove(
          'notebook-raw-data--drag-handle__dragging'
        )
      onUpdateHeight(size)
    }
  }, [isDragging])

  const handleMouseMove = (e: MouseEvent): void => {
    if (!resultsBodyRef.current) {
      return
    }

    const {pageY} = e
    const {top} = resultsBodyRef.current.getBoundingClientRect()

    const updatedHeight = round(
      Math.max(pageY - top, MINIMUM_RESULTS_PANEL_HEIGHT)
    )

    updateSize(updatedHeight)
  }

  const handleMouseDown = (): void => {
    updateDragging(true)
    const body = document.getElementsByTagName('body')[0]
    body && body.classList.add('notebook-results--dragging')

    window.addEventListener('mousemove', handleMouseMove)
    window.addEventListener('mouseup', handleMouseUp)
  }

  const handleMouseUp = (): void => {
    updateDragging(false)
    const body = document.getElementsByTagName('body')[0]
    body && body.classList.remove('notebook-results--dragging')

    window.removeEventListener('mousemove', handleMouseMove)
    window.removeEventListener('mouseup', handleMouseUp)
  }

  return (
    <>
      <ResultsHeader
        resizingEnabled={resizingEnabled}
        visibility={visibility}
        onUpdateVisibility={onUpdateVisibility}
        onStartDrag={handleMouseDown}
        dragHandleRef={dragHandleRef}
      />
      <div className={resultsBodyClassName} ref={resultsBodyRef}>
        {children}
      </div>
    </>
  )
}

export default ResultsResizer
