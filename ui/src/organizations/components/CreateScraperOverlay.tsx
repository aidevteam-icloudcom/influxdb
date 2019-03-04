// Libraries
import React, {PureComponent, ChangeEvent} from 'react'
import {connect} from 'react-redux'
import _ from 'lodash'

// Components
import {Button, ComponentColor, ComponentStatus} from '@influxdata/clockface'
import {
  Form,
  OverlayContainer,
  OverlayHeading,
  OverlayBody,
  OverlayFooter,
  OverlayTechnology,
} from 'src/clockface'
import CreateScraperForm from 'src/organizations/components/CreateScraperForm'

// Actions
import {notify as notifyAction, notify} from 'src/shared/actions/notifications'
import {createScraper} from 'src/organizations/actions/orgView'

// Types
import {Bucket, ScraperTargetRequest} from '@influxdata/influx'
import {
  scraperCreateSuccess,
  scraperCreateFailed,
} from 'src/shared/copy/v2/notifications'

interface OwnProps {
  visible: boolean
  buckets: Bucket[]
  onDismiss: () => void
  overrideBucketIDSelection?: string
}

interface DispatchProps {
  notify: typeof notifyAction
  onCreateScraper: typeof createScraper
}

type Props = OwnProps & DispatchProps

interface State {
  scraper: ScraperTargetRequest
}

class CreateScraperOverlay extends PureComponent<Props, State> {
  constructor(props: Props) {
    super(props)

    this.state = {
      scraper: {
        name: 'My Cool Scraper',
        type: ScraperTargetRequest.TypeEnum.Prometheus,
        url: 'http://localhost:9999/metrics',
        orgID: this.props.buckets[0].organizationID,
        bucketID:
          this.props.overrideBucketIDSelection || this.props.buckets[0].id,
      },
    }
  }

  componentDidUpdate(prevProps) {
    if (
      prevProps.visible === false &&
      this.props.visible === true &&
      this.props.overrideBucketIDSelection
    ) {
      const scraper = {
        ...this.state.scraper,
        bucketID: this.props.overrideBucketIDSelection,
      }
      this.setState({scraper})
    }
  }

  public render() {
    const {scraper} = this.state
    const {onDismiss, buckets, visible} = this.props

    return (
      <OverlayTechnology visible={visible}>
        <OverlayContainer maxWidth={600}>
          <OverlayHeading title="Create Scraper" onDismiss={onDismiss} />
          <Form onSubmit={this.handleSubmit}>
            <OverlayBody>
              <h5 className="wizard-step--sub-title">
                Scrapers collect data from multiple targets at regular intervals
                and to write to a bucket
              </h5>
              <CreateScraperForm
                buckets={buckets}
                url={scraper.url}
                name={scraper.name}
                selectedBucketID={scraper.bucketID}
                onInputChange={this.handleInputChange}
                onSelectBucket={this.handleSelectBucket}
              />
            </OverlayBody>
            <OverlayFooter>
              <Button text="Cancel" onClick={onDismiss} />
              <Button
                status={this.submitButtonStatus}
                text="Create"
                onClick={this.handleSubmit}
                color={ComponentColor.Success}
              />
            </OverlayFooter>
          </Form>
        </OverlayContainer>
      </OverlayTechnology>
    )
  }

  private handleInputChange = (e: ChangeEvent<HTMLInputElement>) => {
    const value = e.target.value
    const key = e.target.name
    const scraper = {...this.state.scraper, [key]: value}

    this.setState({
      scraper,
    })
  }

  private handleSelectBucket = (bucket: Bucket) => {
    const {organizationID, id} = bucket
    const scraper = {...this.state.scraper, orgID: organizationID, bucketID: id}

    this.setState({scraper})
  }

  private get submitButtonStatus(): ComponentStatus {
    const {scraper} = this.state

    if (!scraper.url || !scraper.name || !scraper.bucketID) {
      return ComponentStatus.Disabled
    }

    return ComponentStatus.Default
  }

  private handleSubmit = async () => {
    try {
      const {onCreateScraper, onDismiss, notify} = this.props
      const {scraper} = this.state

      await onCreateScraper(scraper)
      onDismiss()
      notify(scraperCreateSuccess())
    } catch (e) {
      console.error(e)
      notify(scraperCreateFailed())
    }
  }
}

const mdtp: DispatchProps = {
  notify: notifyAction,
  onCreateScraper: createScraper,
}

export default connect<{}, DispatchProps, OwnProps>(
  null,
  mdtp
)(CreateScraperOverlay)
