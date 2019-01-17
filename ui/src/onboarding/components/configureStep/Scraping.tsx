// Libraries
import React, {PureComponent} from 'react'
import {connect} from 'react-redux'

// Components
import {Form} from 'src/clockface'
import FancyScrollbar from 'src/shared/components/fancy_scrollbar/FancyScrollbar'
import OnboardingButtons from 'src/onboarding/components/OnboardingButtons'
import ScraperTarget from 'src/onboarding/components/configureStep/ScraperTarget'

// Actions
import {
  setScraperTargetBucket,
  setScraperTargetURL,
  saveScraperTarget,
} from 'src/onboarding/actions/dataLoaders'

// Types
import {Bucket} from 'src/api'
import {AppState} from 'src/types/v2/index'

interface OwnProps {
  onClickNext: () => void
  onClickBack: () => void
  onClickSkip: () => void
  buckets: Bucket[]
}

interface DispatchProps {
  onSetScraperTargetBucket: typeof setScraperTargetBucket
  onSetScraperTargetURL: typeof setScraperTargetURL
  onSaveScraperTarget: typeof saveScraperTarget
}

interface StateProps {
  scraperBucket: string
  url: string
  currentBucket: string
}

type Props = OwnProps & DispatchProps & StateProps

export class Scraping extends PureComponent<Props> {
  public componentDidMount() {
    const {
      buckets,
      scraperBucket,
      currentBucket,
      onSetScraperTargetBucket,
    } = this.props

    if (!scraperBucket) {
      onSetScraperTargetBucket(currentBucket || buckets[0].name)
    }
  }

  public render() {
    const {
      scraperBucket,
      onClickBack,
      onClickSkip,
      onSetScraperTargetURL,
      url,
    } = this.props

    return (
      <Form onSubmit={this.handleSubmit}>
        <div className="wizard-step--scroll-area">
          <FancyScrollbar autoHide={false}>
            <div className="wizard-step--scroll-content">
              <h3 className="wizard-step--title">Add Scraper Target</h3>
              <h5 className="wizard-step--sub-title">
                Scrapers collect data from multiple targets at regular intervals
                and to write to a bucket
              </h5>
              <ScraperTarget
                bucket={scraperBucket}
                buckets={this.buckets}
                onSelectBucket={this.handleSelectBucket}
                onChangeURL={onSetScraperTargetURL}
                url={url}
              />
            </div>
          </FancyScrollbar>
        </div>
        <OnboardingButtons
          onClickBack={onClickBack}
          onClickSkip={onClickSkip}
          showSkip={true}
          autoFocusNext={false}
          skipButtonText={'Skip'}
        />
      </Form>
    )
  }

  private get buckets(): string[] {
    const {buckets} = this.props

    return buckets.map(b => b.name)
  }

  private handleSelectBucket = (bucket: string) => {
    this.props.onSetScraperTargetBucket(bucket)
  }

  private handleSubmit = async () => {
    await this.props.onSaveScraperTarget()

    this.props.onClickNext()
  }
}

const mstp = ({
  dataLoading: {
    dataLoaders: {scraperTarget},
    steps: {bucket},
  },
}: AppState): StateProps => {
  return {
    currentBucket: bucket,
    scraperBucket: scraperTarget.bucket,
    url: scraperTarget.url,
  }
}

const mdtp: DispatchProps = {
  onSetScraperTargetBucket: setScraperTargetBucket,
  onSetScraperTargetURL: setScraperTargetURL,
  onSaveScraperTarget: saveScraperTarget,
}

export default connect<StateProps, DispatchProps, OwnProps>(
  mstp,
  mdtp
)(Scraping)
