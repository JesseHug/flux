import type { ThemePackage } from "../types";

import stardewCss from "./stardew.css?raw";

function setThemeAttribute(active: boolean) {
  if (typeof document === "undefined") return;
  if (active) {
    document.documentElement.setAttribute("data-flux-theme", "stardew");
    document.body?.setAttribute("data-flux-theme", "stardew");
  } else {
    if (
      document.documentElement.getAttribute("data-flux-theme") === "stardew"
    ) {
      document.documentElement.removeAttribute("data-flux-theme");
    }
    if (document.body?.getAttribute("data-flux-theme") === "stardew") {
      document.body.removeAttribute("data-flux-theme");
    }
  }
}

const stardewTheme: ThemePackage = {
  id: "stardew",
  name: "星露谷农场",
  author: "FLUX",
  version: "1.0.0",
  description: "星露谷像素庄园、16种可换装小屋与四季昼夜配乐",

  tokens: {
    light: {
      "--background": "#fef1d1",
      "--foreground": "#3e3026",
      "--border": "#c8ab7e",
      "--input": "#c8ab7e",
      "--ring": "#356445",
      "--content1": "#fff9ec",
      "--divider": "#dfc9a4",

      "--default-50": "#fcf3dc",
      "--default-100": "#f5e8cc",
      "--default-200": "#e7d3ab",
      "--default-300": "#cfb382",
      "--default-400": "#917c66",
      "--default-500": "#6e5b49",
      "--default-600": "#584637",
      "--default-700": "#46372b",
      "--default-800": "#3e3026",
      "--default-900": "#291e17",

      "--primary": "#356445",
      "--primary-foreground": "#fffaf0",
      "--primary-50": "#edf5ef",
      "--primary-100": "#d5e8da",
      "--primary-200": "#aed1b7",
      "--primary-300": "#7eb38c",
      "--primary-400": "#508d62",
      "--primary-500": "#356445",
      "--primary-600": "#2b5239",
      "--primary-700": "#22422e",
      "--primary-800": "#1b3425",
      "--primary-900": "#14271c",

      "--secondary": "#8c5630",
      "--secondary-foreground": "#fffaf0",
      "--secondary-50": "#faf1ea",
      "--secondary-100": "#f2dfd0",
      "--secondary-200": "#e2bfa3",
      "--secondary-300": "#ce9970",
      "--secondary-400": "#b17549",
      "--secondary-500": "#8c5630",
      "--secondary-600": "#754524",
      "--secondary-700": "#5f361c",
      "--secondary-800": "#4b2a16",
      "--secondary-900": "#381f10",

      "--success": "#357a48",
      "--success-50": "#eef7f0",
      "--success-100": "#d4ebd9",
      "--success-200": "#a9d7b5",
      "--success-300": "#77be8b",
      "--success-400": "#4ea066",
      "--success-500": "#357a48",
      "--success-600": "#2a623a",
      "--success-700": "#224e2e",
      "--success-800": "#1b3d24",
      "--success-900": "#142e1b",

      "--warning": "#b86b11",
      "--warning-50": "#fdf5e8",
      "--warning-100": "#fae6c6",
      "--warning-200": "#f3cc8d",
      "--warning-300": "#eaad52",
      "--warning-400": "#dc8c24",
      "--warning-500": "#b86b11",
      "--warning-600": "#96540c",
      "--warning-700": "#78420a",
      "--warning-800": "#5e3308",
      "--warning-900": "#472606",

      "--danger": "#a63a30",
      "--danger-50": "#fbeeed",
      "--danger-100": "#f6d6d3",
      "--danger-200": "#eba8a2",
      "--danger-300": "#dc776f",
      "--danger-400": "#c44f45",
      "--danger-500": "#a63a30",
      "--danger-600": "#872d25",
      "--danger-700": "#6d241d",
      "--danger-800": "#561c17",
      "--danger-900": "#401410",

      "--radius": "0.25rem",
      "--radius-sm": "0.1875rem",
      "--radius-lg": "0.3125rem",
    },

    dark: {
      "--background": "#0c1926",
      "--foreground": "#f6eedf",
      "--border": "#3e4f5e",
      "--input": "#3e4f5e",
      "--ring": "#e8b86d",
      "--content1": "#1b2936",
      "--divider": "#2c3d4b",

      "--default-50": "#162330",
      "--default-100": "#233342",
      "--default-200": "#2e4152",
      "--default-300": "#3e4f5e",
      "--default-400": "#8393a0",
      "--default-500": "#b8c1c8",
      "--default-600": "#d1d5d4",
      "--default-700": "#e4dfd5",
      "--default-800": "#efe7d8",
      "--default-900": "#f6eedf",

      "--primary": "#e8b86d",
      "--primary-foreground": "#1c140d",
      "--primary-50": "#2a2216",
      "--primary-100": "#3d301e",
      "--primary-200": "#5c482c",
      "--primary-300": "#85673c",
      "--primary-400": "#b58b4f",
      "--primary-500": "#e8b86d",
      "--primary-600": "#efc686",
      "--primary-700": "#f5d4a0",
      "--primary-800": "#fae2bc",
      "--primary-900": "#fdf0da",

      "--secondary": "#4c7499",
      "--secondary-foreground": "#f6eedf",
      "--secondary-50": "#152330",
      "--secondary-100": "#1f3447",
      "--secondary-200": "#2c4963",
      "--secondary-300": "#3b5f80",
      "--secondary-400": "#4c7499",
      "--secondary-500": "#648eb3",
      "--secondary-600": "#84a9c9",
      "--secondary-700": "#a5c3de",
      "--secondary-800": "#c7dcf0",
      "--secondary-900": "#e6f0fa",

      "--success": "#6fd18d",
      "--success-50": "#14291d",
      "--success-100": "#1e3d2b",
      "--success-200": "#2c5b40",
      "--success-300": "#3f825b",
      "--success-400": "#56ab77",
      "--success-500": "#6fd18d",
      "--success-600": "#8adba3",
      "--success-700": "#a7e4ba",
      "--success-800": "#c5edd1",
      "--success-900": "#e3f6e9",

      "--warning": "#f3c486",
      "--warning-50": "#2c2214",
      "--warning-100": "#42321d",
      "--warning-200": "#634b2a",
      "--warning-300": "#8d6b3b",
      "--warning-400": "#bd9356",
      "--warning-500": "#f3c486",
      "--warning-600": "#f6d09c",
      "--warning-700": "#f8dcb4",
      "--warning-800": "#fbe8cd",
      "--warning-900": "#fdf4e6",

      "--danger": "#ff8a80",
      "--danger-50": "#2e1615",
      "--danger-100": "#472120",
      "--danger-200": "#6e3230",
      "--danger-300": "#9e4844",
      "--danger-400": "#d1645e",
      "--danger-500": "#ff8a80",
      "--danger-600": "#ffa299",
      "--danger-700": "#ffb9b3",
      "--danger-800": "#ffd1cc",
      "--danger-900": "#ffe8e6",

      "--radius": "0.25rem",
      "--radius-sm": "0.1875rem",
      "--radius-lg": "0.3125rem",
    },
  },

  css: stardewCss,

  onActivate: () => {
    setThemeAttribute(true);
  },
  onDeactivate: () => {
    setThemeAttribute(false);
  },
};

export default stardewTheme;
