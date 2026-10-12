import { useRef, useState } from 'react'
import { QRCodeSVG } from 'qrcode.react'

export default function ShareCard({ slug }) {
  const url = `${window.location.origin}/p/${slug}`
  const input = useRef(null)
  const [copied, setCopied] = useState(false)
  const [showQr, setShowQr] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(url)
    } catch {
      // Clipboard API needs https; fall back to selecting the text for manual copy.
      input.current?.select()
      document.execCommand?.('copy')
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <section className="card share" aria-label="Share this poll">
      <h2>Share this poll</h2>
      <p className="muted">Anyone with this link can vote. They don’t need an account.</p>
      <div className="share-row">
        <input ref={input} readOnly value={url} onFocus={(e) => e.target.select()} aria-label="Poll link" />
        <button className="btn btn-primary" onClick={copy}>
          {copied ? 'Copied' : 'Copy link'}
        </button>
      </div>
      <button className="linklike" onClick={() => setShowQr((s) => !s)} aria-expanded={showQr}>
        {showQr ? 'Hide QR code' : 'Show QR code'}
      </button>
      {showQr && (
        <div className="qr">
          <QRCodeSVG value={url} size={176} marginSize={2} />
          <p className="muted small">Point a phone camera here to open the poll, handy for a room full of people.</p>
        </div>
      )}
    </section>
  )
}
