import mark from '../assets/flamingo-mark.png'

// The flamingo-and-speech-bubble mark with the wordmark beside it.
export function Brand({ big }: { big?: boolean }) {
  return (
    <div className={`brand ${big ? 'brand--big' : ''}`}>
      <img className="brand__mark" src={mark} alt="" width={big ? 60 : 44} height={big ? 60 : 44} />
      <span className="brand__name">Flamingo</span>
    </div>
  )
}
