export function CountryFlag({ country }: { country: "BR" | "PY" | null }) {
  if (!country) return <span className="muted-line">—</span>;

  if (country === "BR") {
    return (
      <svg className="country-flag" viewBox="0 0 20 14" role="img" aria-label="Brasil">
        <title>Brasil</title>
        <rect width="20" height="14" fill="#009c3b" />
        <polygon points="10,1.5 18.5,7 10,12.5 1.5,7" fill="#ffdf00" />
        <circle cx="10" cy="7" r="3" fill="#002776" />
      </svg>
    );
  }

  return (
    <svg className="country-flag" viewBox="0 0 20 14" role="img" aria-label="Paraguai">
      <title>Paraguai</title>
      <rect width="20" height="4.67" fill="#d52b1e" />
      <rect y="4.67" width="20" height="4.67" fill="#ffffff" />
      <rect y="9.33" width="20" height="4.67" fill="#0038a8" />
    </svg>
  );
}
