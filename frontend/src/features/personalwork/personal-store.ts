import { createViewOperationStore } from "../searchviews/operation-state";
import { createPortfolioStore } from "./portfolio-state";
import type {
  FavoriteCommand,
  PortfolioValues,
  PortfolioPage,
} from "./personal.service";
export const favoriteOperations = createViewOperationStore<FavoriteCommand>();
export const portfolioCaptures = createPortfolioStore<
  PortfolioValues,
  PortfolioPage
>();
