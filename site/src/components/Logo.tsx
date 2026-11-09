import { brand } from '../brand'

type LogoProps = {
  wordmark?: boolean
  height?: number
}

export function Logo({ wordmark = true, height = 30 }: LogoProps) {
  return (
    <span className="logo" role="img" aria-label="Avior">
      <svg viewBox="0 0 64 64" height={height} aria-hidden="true">
        <path
          fill={brand.amber}
          d="M27 6 L30.394 21.606 L44 25 L30.394 28.394 L27 61 L23.606 28.394 L10 25 L23.606 21.606 Z"
        />
        <circle fill={brand.blue} cx="51" cy="10" r="4.5" />
      </svg>
      {wordmark ? (
        <svg
          viewBox="-2 -2 115 28"
          height={Math.round(height * 0.47)}
          fill="none"
          stroke={brand.ink}
          strokeWidth="2.4"
          strokeLinecap="square"
          strokeLinejoin="miter"
          aria-hidden="true"
        >
          <path d="M0 24 L9 0 L18 24" />
          <path transform="translate(27 0)" d="M0 0 L9 24 L18 0" />
          <path transform="translate(54 0)" d="M0 0 V24" />
          <path transform="translate(63 0)" d="M11 0 A11 12 0 1 1 10.99 0 Z" />
          <path transform="translate(94 0)" d="M0 24 V0 H8 A6.5 6.5 0 0 1 8 13 H0 M7.5 13 L15 24" />
        </svg>
      ) : null}
    </span>
  )
}
