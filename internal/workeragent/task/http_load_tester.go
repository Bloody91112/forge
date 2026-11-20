package task

import "context"

type HTTPLoadTester struct {
}

func (t *HTTPLoadTester) HandleTask(ctx context.Context, task Task) (ServiceResult, error) {
	return ServiceResult{}, nil
}
