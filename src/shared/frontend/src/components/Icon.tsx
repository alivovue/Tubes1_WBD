import icons from "../assets/icons.svg?no-inline";
export function Icon({ name }: { name: string }) {
  return (
    <svg aria-hidden="true">
      <use href={`${icons}#icon-${name}`} />
    </svg>
  );
}
