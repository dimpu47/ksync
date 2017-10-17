package ksync

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type ServiceList struct {
	Items []*Service
}

func GetServices() *ServiceList {
	list := &ServiceList{}

	list.Get()

	return list
}

func (s *ServiceList) Get() error {
	args := filters.NewArgs()
	args.Add("label", "heritage=ksync")

	cntrs, err := dockerClient.ContainerList(
		context.Background(),
		types.ContainerListOptions{
			Filters: args,
		},
	)

	// TODO: is this even possible?
	if err != nil {
		return errors.Wrap(err, "could not get container list from docker.")
	}

	for _, cntr := range cntrs {
		service := &Service{
			Name: cntr.Labels["name"],
			Container: &Container{
				PodName:  cntr.Labels["pod"],
				Name:     cntr.Labels["container"],
				NodeName: cntr.Labels["node"],
			},
		}
		s.Items = append(s.Items, service)
		log.WithFields(service.Fields()).Debug("found service")
	}

	return nil
}

// Normalize starts services for any specs that don't have ones and stops the
// services that are no longer required.
func (s *ServiceList) Normalize() error {
	specs, _ := AllSpecs()

	if err := s.compact(specs); err != nil {
		return err
	}

	if err := s.update(specs); err != nil {
		return err
	}

	return nil
}

func (s *ServiceList) Filter(name string) *ServiceList {
	list := &ServiceList{}
	for _, service := range s.Items {
		list.Items = append(list.Items, service)
	}

	return list
}

func (s *ServiceList) Stop() error {
	for _, service := range s.Items {
		if err := service.Stop(); err != nil {
			return err
		}
	}

	return nil
}

func (s *ServiceList) compact(specs *SpecMap) error {
	for _, service := range s.Items {
		if _, ok := specs.Items[service.Name]; ok {
			continue
		}

		if err := service.Stop(); err != nil {
			return errors.Wrap(
				err, "unable to stop service that is no longer needed.")
		}
	}

	return nil
}

func (s *ServiceList) update(specs *SpecMap) error {
	for name, spec := range specs.Items {
		containerList, err := GetContainers(
			spec.Pod, spec.Selector, spec.Container)
		if err != nil {
			return ErrorOut("unable to get container list", err, nil)
		}

		if len(containerList) == 0 {
			log.WithFields(spec.Fields()).Debug("no matching running containers.")

			if err := s.Filter(name).Stop(); err != nil {
				return err
			}
			continue
		}

		// TODO: should this be on its own?
		for _, cntr := range containerList {
			if err := NewService(name, cntr, spec).Start(); err != nil {
				if IsServiceRunning(err) {
					log.WithFields(
						MergeFields(cntr.Fields(), spec.Fields())).Debug("already running")
					continue
				}

				return err
			}
		}
	}

	return nil
}